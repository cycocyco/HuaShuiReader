package legado

import (
	"strings"
	"testing"
)

func TestJSRuntime_ExecutionAndSecurity(t *testing.T) {
	runtime := NewJSRuntime(nil)

	// 1. 常规 JS 与 Java 工具函数
	script := `
		var b64 = java.base64Encode("摸鱼小工具");
		var decoded = java.base64Decode(b64);
		var md5Val = java.md5Encode("123456");
		decoded + "|" + md5Val;
	`
	res, err := runtime.Execute(script, nil, "")
	if err != nil {
		t.Fatalf("Execute basic script failed: %v", err)
	}
	resStr, ok := res.(string)
	if !ok || !strings.HasPrefix(resStr, "摸鱼小工具|") {
		t.Fatalf("Script result mismatch: %v", res)
	}

	// 2. 超时中断测试 (死循环)
	infiniteScript := `
		var i = 0;
		while (true) {
			i++;
		}
	`
	_, errInf := runtime.Execute(infiniteScript, nil, "")
	if errInf == nil || !strings.Contains(errInf.Error(), "timed out") {
		t.Fatalf("Expected timeout error for infinite loop, got: %v", errInf)
	}

	// 3. SSRF 回环拦截测试
	ssrfScript := `
		java.ajax("http://127.0.0.1:8080/secret");
	`
	resSSRF, err := runtime.Execute(ssrfScript, nil, "")
	if err != nil {
		t.Fatalf("SSRF script should not panic: %v", err)
	}
	if resSSRF != "" {
		t.Fatalf("Expected empty result for blocked loopback IP, got: %v", resSSRF)
	}
}
