# bypass-deepseek-enhanced

基于 [xllm-go/bypass](https://github.com/xllm-go/bypass) 的 DeepSeek 网页端 API 增强版，支持快速、专家、搜索、识图模式及流式思考分离。

An enhanced DeepSeek Web API adapter based on [xllm-go/bypass](https://github.com/xllm-go/bypass), featuring fast, expert, search, and vision modes with proper reasoning separation in streaming responses.

> 本仓库是上游项目的社区维护增强版，不是 DeepSeek 官方项目。
>
> This is a community-maintained enhanced fork. It is not an official DeepSeek project.

## 主要增强 / Highlights

- 适配新版 DeepSeek Web 请求与响应格式。
- 支持快速、专家、联网搜索和图片识别模式。
- 支持思考模式与无思考模式的独立模型别名。
- 将 `THINK` 片段输出到 OpenAI 兼容的 `reasoning_content`，将最终回答保留在 `content`。
- 修复流式 JSON Patch 中无路径增量继承以及最终回答首字符丢失问题。
- 提供本地 PoW 求解与图片上传适配，PoW 请求头使用与浏览器 `btoa` 一致的标准 Base64。
- 标准 OpenAI `tools` 请求默认启用工具选择，并兼容 DeepSeek 当前 JSON Patch/SSE 工具响应。
- 对请求日志中的 Authorization、API Key 和 Cookie 等敏感请求头进行脱敏。
- 增加 DeepSeek 模式、PoW、图片、流式及非流式响应的回归测试。

## 模型 / Models

| Model ID | Web mode | Thinking | Search | Vision |
| --- | --- | ---: | ---: | ---: |
| `deepseek-chat` | Fast (legacy alias) | No | No | No |
| `deepseek-reasoner` | Fast (legacy alias) | Yes | No | No |
| `deepseek-v4-flash` | Fast | Yes | No | No |
| `deepseek-v4-flash-nothinking` | Fast | No | No | No |
| `deepseek-v4-pro` | Expert | Yes | No | No |
| `deepseek-v4-pro-nothinking` | Expert | No | No | No |
| `deepseek-v4-flash-search` | Fast | Yes | Yes | No |
| `deepseek-v4-flash-search-nothinking` | Fast | No | Yes | No |
| `deepseek-v4-vision` | Vision | Yes | No | Yes |
| `deepseek-v4-vision-nothinking` | Vision | No | No | Yes |

## 构建 / Build

项目使用 Go 1.26.5，并沿用上游的 `iocgo` 构建流程。

```shell
go install ./cmd/iocgo
go build -toolexec iocgo ./main.go
```

也可以使用 Makefile：

```shell
make install
make build-linux
```

仓库的 GitHub Actions 会验证 `linux/amd64` 和 `linux/arm64` 镜像。合并到 `main` 或推送 `v*` 标签后，会将镜像发布到 `ghcr.io/aaron6769/bypass-deepseek-enhanced`。

## 测试 / Test

```shell
go test ./core/common/toolcall ./core/gin ./relay/llm/deepseek
```

## 配置与使用 / Configuration

配置结构、启动方式和其他适配器说明请参考[上游文档](https://bincooo.github.io/chatgpt-adapter)。本项目保持 OpenAI Chat Completions 兼容接口。

请勿提交真实的 `config.yaml`、`.env`、Cookie、Token、账号密码或部署密钥。本仓库的 `.gitignore` 已默认排除常见本地配置和构建产物。

### 图片输入安全 / Image input safety

- 单张图片最大 20 MiB，支持图片 `data:` URI 和公开的 HTTP(S) URL。
- URL 下载会拒绝回环、私网、链路本地、云元数据及特殊转换网段，并对每次重定向重新解析和校验，最多跟随 3 次。
- 图片下载代理支持 HTTP、SOCKS5 和 SOCKS5H。HTTPS 代理因目标证书固定需要独立 TLS 配置，当前会明确拒绝。
- 上述代理限制仅适用于从 URL 下载图片，不改变其他适配器请求所使用的 `server.proxied` 行为。
- You.com 的定时检查只使用本地配置中的 `you.cookies`，仓库不提供或回退到内置账号 Cookie。

## 上游与修改记录 / Upstream and changes

- Upstream repository: [xllm-go/bypass](https://github.com/xllm-go/bypass)
- Original project documentation: [ChatGPT Adapter docs](https://bincooo.github.io/chatgpt-adapter)
- Detailed changes: [MODIFICATIONS.md](MODIFICATIONS.md)

感谢上游作者及所有贡献者。本仓库仅对修改部分负责，并保留上游的版权与许可证声明。

## 免责声明 / Disclaimer

本项目仅用于测试、学习和研究。网页接口可能随时变化，不能保证其合法性、准确性、稳定性、完整性或长期可用性。使用者应自行确认并遵守所在地区法律、服务条款和账号规则，不得用于非法用途。

上游 README 还包含非商业使用和资源转载限制等特别声明。为降低许可及使用风险，使用本项目时也应阅读并尊重[上游仓库的完整声明](https://github.com/xllm-go/bypass#%E7%89%B9%E5%88%AB%E5%A3%B0%E6%98%8E)。

This project is intended for testing, learning, and research. Web APIs may change without notice. Users are responsible for complying with applicable laws, service terms, and account policies.

## License

本项目基于 GPL-3.0 授权，修改版继续使用相同许可证。详情请参阅 [LICENSE](LICENSE)。

This modified work remains licensed under the GNU General Public License v3.0. See [LICENSE](LICENSE).
