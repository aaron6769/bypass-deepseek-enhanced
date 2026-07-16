package you

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"chatgpt-adapter/core/common"
	"chatgpt-adapter/core/common/inited"
	"chatgpt-adapter/core/common/vars"
	"chatgpt-adapter/core/gin/response"
	"chatgpt-adapter/core/logger"
	"github.com/bincooo/emit.io"
	"github.com/bincooo/you.com"
	"github.com/gin-gonic/gin"
	"github.com/iocgo/sdk/env"
	"github.com/iocgo/sdk/proxy"
)

var (
	mu sync.Mutex

	lang      = "cn-ZN,cn;q=0.9"
	clearance = ""
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36 Edg/125.0.0.0"

	cookiesContainer *common.PollContainer[string]
)

func init() {
	inited.AddInitialized(func(env *env.Environment) {
		cookies := env.GetStringSlice("you.cookies")
		cookiesContainer = common.NewPollContainer[string]("you", cookies, 6*time.Hour)
		cookiesContainer.Condition = condition(env)
		if len(cookies) > 0 && env.GetBool("you.task") {
			go timer(env, cookies[0])
		}
	})
}

func timer(env *env.Environment, cookie string) {
	m30 := 30 * time.Minute

	for {
		time.Sleep(m30)
		if clearance != "" {
			timeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			chat := you.New(cookie, you.GPT_4, env.GetString("server.proxied"))
			chat.CloudFlare(clearance, userAgent, lang)
			chat.Client(common.HTTPClient)
			_, err := chat.State(timeout)
			cancel()
			if err == nil {
				continue
			}

			var se emit.Error
			if !errors.As(err, &se) {
				logger.Errorf("定时器 you.com 过盾检查失败：%v", err)
				continue
			}

			if se.Code == 403 {
				// 需要重新过盾
				clearance = ""
			} else {
				logger.Info("定时器执行 you.com 过盾检查，无需执行")
				continue
			}
		}

		// 尝试过盾
		if err := hookCloudflare(env); err != nil {
			logger.Errorf("you.com 尝试过盾失败：%v", err)
			continue
		}

		logger.Info("定时器执行 you.com 过盾成功")
	}
}

func InvocationHandler(ctx *proxy.Context) {
	var (
		gtx  = ctx.In[0].(*gin.Context)
		echo = gtx.GetBool(vars.GinEcho)
	)

	if echo || ctx.Method != "Completion" && ctx.Method != "ToolChoice" {
		ctx.Do()
		return
	}

	logger.Infof("execute static proxy [relay/llm/you.api]: func %s(...)", ctx.Method)

	if cookiesContainer.Len() == 0 {
		response.Error(gtx, -1, "empty cookies")
		return
	}

	cookies, err := cookiesContainer.Poll()
	if err != nil {
		logger.Error(err)
		response.Error(gtx, -1, err)
		return
	}
	defer resetMarked(cookies)
	gtx.Set("token", cookies)
	gtx.Set("clearance", clearance)
	gtx.Set("userAgent", userAgent)
	gtx.Set("lang", lang)

	//
	ctx.Do()

	//
	if ctx.Method == "Completion" {
		err = elseOf[error](ctx.Out[0])
	}
	if ctx.Method == "ToolChoice" {
		err = elseOf[error](ctx.Out[1])
	}

	if err != nil {
		logger.Error(err)
		var se emit.Error
		if errors.As(err, &se) && se.Code > 400 {
			_ = cookiesContainer.MarkTo(cookies, 2)
			// 403 重定向？？？
			if se.Code == 403 {
				cleanCloudflare()
			}
		}

		if strings.Contains(err.Error(), "ZERO QUOTA") {
			_ = cookiesContainer.MarkTo(cookies, 2)
		}
		return
	}
}

func condition(env *env.Environment) func(string, ...interface{}) bool {
	return func(cookies string, argv ...interface{}) bool {

		marker, err := cookiesContainer.Marked(cookies)
		if err != nil {
			logger.Error(err)
			return false
		}

		if marker != 0 {
			return false
		}

		// return true
		chat := you.New(cookies, you.CLAUDE_2, env.GetString("server.proxied"))
		chat.Client(common.HTTPClient)
		chat.CloudFlare(clearance, userAgent, lang)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// 检查可用次数
		count, err := chat.State(ctx)
		if err != nil {
			var se emit.Error
			if errors.As(err, &se) {
				if se.Code == 403 {
					cleanCloudflare()
					_ = hookCloudflare(env)
				}
				if se.Code == 401 { // cookie 失效？？？
					_ = cookiesContainer.MarkTo(cookies, 2)
				}
			}
			logger.Error(err)
			return false
		}

		if count <= 0 {
			_ = cookiesContainer.MarkTo(cookies, 2)
			return false
		}

		return true
	}
}

func resetMarked(cookies string) {
	marker, err := cookiesContainer.Marked(cookies)
	if err != nil {
		logger.Error(err)
		return
	}

	if marker != 1 {
		return
	}

	err = cookiesContainer.MarkTo(cookies, 0)
	if err != nil {
		logger.Error(err)
	}
}

func hookCloudflare(env *env.Environment) error {
	if clearance != "" {
		return nil
	}

	baseUrl := env.GetString("browser-less.reversal")
	if !env.GetBool("browser-less.enabled") && baseUrl == "" {
		return errors.New("trying cloudflare failed, please setting `browser-less.enabled` or `browser-less.reversal`")
	}

	logger.Info("trying cloudflare ...")

	mu.Lock()
	defer mu.Unlock()
	if clearance != "" {
		return nil
	}

	if baseUrl == "" {
		baseUrl = "http://127.0.0.1:" + env.GetString("browser-less.port")
	}

	r, err := emit.ClientBuilder(common.HTTPClient).
		GET(baseUrl+"/clearance").
		DoC(emit.Status(http.StatusOK), emit.IsJSON)
	if err != nil {
		logger.Error(err)
		if emit.IsJSON(r) == nil {
			logger.Error(emit.TextResponse(r))
		}
		return err
	}

	defer r.Body.Close()
	obj, err := emit.ToMap(r)
	if err != nil {
		logger.Error(err)
		return err
	}

	data := obj["data"].(map[string]interface{})
	clearance = data["cookie"].(string)
	userAgent = data["userAgent"].(string)
	lang = data["lang"].(string)
	return nil
}

func cleanCloudflare() {
	mu.Lock()
	clearance = ""
	mu.Unlock()
}

func elseOf[T any](obj any) (zero T) {
	if obj == nil {
		return
	}
	return obj.(T)
}
