package legado

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// JSRuntime Legado JavaScript 执行沙箱（安全加固版）
type JSRuntime struct {
	client *http.Client
}

func NewJSRuntime(client *http.Client) *JSRuntime {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &JSRuntime{client: client}
}

// isPrivateOrLoopbackHost 校验是否为私有回环地址或内网地址（防止 SSRF 漏洞）
func isPrivateOrLoopbackHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	if ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
	}
	return false
}

// Execute 执行一段 Legado 内嵌 JS，并返回字符串或对象结果（包含 2.5s 超时硬中断与 SSRF 拦截）
func (r *JSRuntime) Execute(script string, resultVal interface{}, baseURL string) (interface{}, error) {
	vm := goja.New()

	// 2.5 秒硬超时监控，超时强制打断 Goja VM，杜绝书源死循环卡死
	timer := time.AfterFunc(2500*time.Millisecond, func() {
		vm.Interrupt("JavaScript execution timed out (2.5s limit exceeded)")
	})
	defer timer.Stop()

	// 注入环境基础变量
	vm.Set("result", resultVal)
	vm.Set("baseUrl", baseURL)

	// 注入模拟的 java 扩展对象 (与阅读 3.0 完全对齐)
	javaObj := vm.NewObject()

	// 1. 网络请求: java.ajax(url)
	javaObj.Set("ajax", func(call goja.FunctionCall) goja.Value {
		targetURL := call.Argument(0).String()
		if targetURL == "" {
			return vm.ToValue("")
		}
		// 相对路径补全
		if baseURL != "" && !strings.HasPrefix(targetURL, "http") {
			if base, err := url.Parse(baseURL); err == nil {
				if rel, err := base.Parse(targetURL); err == nil {
					targetURL = rel.String()
				}
			}
		}

		parsed, err := url.Parse(targetURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return vm.ToValue("")
		}
		// SSRF 防御拦截
		if isPrivateOrLoopbackHost(parsed.Host) {
			return vm.ToValue("")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
		if err != nil {
			return vm.ToValue("")
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

		resp, err := r.client.Do(req)
		if err != nil {
			return vm.ToValue("")
		}
		defer resp.Body.Close()

		// 限制最大读取 2MB 响应，防止内存爆炸
		lr := io.LimitReader(resp.Body, 2*1024*1024)
		data, err := io.ReadAll(lr)
		if err != nil {
			return vm.ToValue("")
		}
		return vm.ToValue(string(data))
	})

	// 2. 增强连接: java.connect(url, options)
	javaObj.Set("connect", func(call goja.FunctionCall) goja.Value {
		ajaxFn, _ := goja.AssertFunction(javaObj.Get("ajax"))
		res, _ := ajaxFn(goja.Undefined(), call.Arguments...)
		return res
	})

	// 3. Base64 解码与编码
	javaObj.Set("base64Decode", func(call goja.FunctionCall) goja.Value {
		raw := call.Argument(0).String()
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return vm.ToValue(raw)
		}
		return vm.ToValue(string(decoded))
	})

	javaObj.Set("base64DecodeToByteArray", func(call goja.FunctionCall) goja.Value {
		raw := call.Argument(0).String()
		decoded, _ := base64.StdEncoding.DecodeString(raw)
		return vm.ToValue(decoded)
	})

	javaObj.Set("base64Encode", func(call goja.FunctionCall) goja.Value {
		raw := call.Argument(0).String()
		encoded := base64.StdEncoding.EncodeToString([]byte(raw))
		return vm.ToValue(encoded)
	})

	// 4. MD5 加密
	javaObj.Set("md5Encode", func(call goja.FunctionCall) goja.Value {
		raw := call.Argument(0).String()
		h := md5.Sum([]byte(raw))
		return vm.ToValue(hex.EncodeToString(h[:]))
	})

	// 5. 日志打印
	javaObj.Set("log", func(call goja.FunctionCall) goja.Value {
		return goja.Undefined()
	})

	// 挂载全局别名
	vm.Set("java", javaObj)
	vm.Set("baseUrl", baseURL)

	val, err := vm.RunString(script)
	if err != nil {
		return nil, err
	}
	return val.Export(), nil
}
