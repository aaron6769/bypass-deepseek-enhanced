package deepseek

import (
	"chatgpt-adapter/core/common"
	"chatgpt-adapter/core/gin/inter"
	"chatgpt-adapter/core/gin/model"
	"chatgpt-adapter/core/gin/response"
	"chatgpt-adapter/core/logger"
	"github.com/gin-gonic/gin"
	"github.com/iocgo/sdk/env"
)

var (
	Model = "deepseek"
)

type api struct {
	inter.BaseAdapter

	env *env.Environment
}

type deepSeekConnection struct {
	Proxied string
	Cookie  string
}

func (api *api) connection(ctx *gin.Context) deepSeekConnection {
	return deepSeekConnection{
		Proxied: api.env.GetString("server.proxied"),
		Cookie:  ctx.GetString("token"),
	}
}

func (api *api) Match(ctx *gin.Context, model string) (ok bool, err error) {
	_, ok = resolveDeepSeekMode(model)
	return
}

func (api *api) Models() (slice []model.Model) {
	for _, id := range deepSeekModelOrder {
		slice = append(slice, model.Model{
			Id:      id,
			Object:  "model",
			Created: 1686935002,
			By:      Model + "-adapter",
		})
	}
	return
}

func (api *api) ToolChoice(ctx *gin.Context) (ok bool, err error) {
	var (
		connection = api.connection(ctx)
		completion = common.GetGinCompletion(ctx)
	)

	if toolChoice(ctx, api.env, connection, completion) {
		ok = true
	}
	return
}

func (api *api) Completion(ctx *gin.Context) (err error) {
	var (
		connection = api.connection(ctx)
		completion = common.GetGinCompletion(ctx)
	)

	request, err := convertRequest(ctx, api.env, completion)
	if err != nil {
		logger.Error(err)
		return
	}
	defer deleteSession(connection, request.ChatSessionId)

	r, err := fetch(ctx.Request.Context(), connection, request)
	if err != nil {
		logger.Error(err)
		return
	}

	content := waitResponse(ctx, r, completion.Stream)
	if content == "" && response.NotResponse(ctx) {
		response.Error(ctx, -1, "EMPTY RESPONSE")
	}
	return
}
