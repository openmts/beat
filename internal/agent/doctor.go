package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type DoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type DoctorReport struct {
	GeneratedAt time.Time     `json:"generatedAt"`
	Healthy     bool          `json:"healthy"`
	Checks      []DoctorCheck `json:"checks"`
}

func Diagnose(serverURL, token, stateFile, diskPath string) DoctorReport {
	report := DoctorReport{GeneratedAt: time.Now().UTC()}
	add := func(name, status, detail string) {
		report.Checks = append(report.Checks, DoctorCheck{Name: name, Status: status, Detail: detail})
	}

	if runtime.GOOS != "linux" {
		add("platform", "warn", "当前平台为 "+runtime.GOOS+"，正式 Agent 采集器面向 Linux")
	} else {
		add("platform", "ok", "linux/"+runtime.GOARCH)
	}
	if strings.TrimSpace(token) == "" {
		add("token", "fail", "未提供 --token；请从节点接入页面生成短期令牌")
	} else if len(token) < 16 {
		add("token", "fail", "令牌长度异常，请重新生成接入命令")
	} else {
		add("token", "ok", "已配置接入令牌")
	}

	checkServerEndpoint(serverURL, add)
	if _, err := NewCollector(diskPath).Collect(); err != nil {
		add("collector", "fail", "采集器自检失败: "+err.Error())
	} else {
		add("collector", "ok", "CPU、内存、磁盘与网络指标可读取")
	}
	checkStateFile(stateFile, add)
	for _, check := range report.Checks {
		if check.Status == "fail" {
			report.Healthy = false
			return report
		}
	}
	report.Healthy = true
	return report
}

func checkServerEndpoint(serverURL string, add func(string, string, string)) {
	target, secure, err := grpcTarget(serverURL)
	if err != nil {
		add("server", "fail", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dialer := &net.Dialer{}
	if secure {
		parsed, _ := url.Parse(serverURL)
		if !strings.Contains(serverURL, "://") {
			parsed, _ = url.Parse("https://" + serverURL)
		}
		host := parsed.Hostname()
		conn, err := tls.DialWithDialer(dialer, "tcp", target, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host})
		if err != nil {
			add("server", "fail", "TLS 连接失败: "+err.Error())
			return
		}
		_ = conn.Close()
		add("server", "ok", "TLS 端点可达: "+target)
		return
	}
	conn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		add("server", "fail", "无法连接 Server: "+err.Error())
		return
	}
	_ = conn.Close()
	add("server", "ok", "gRPC 端点可达: "+target+"（h2c）")
}

func checkStateFile(stateFile string, add func(string, string, string)) {
	stateFile = strings.TrimSpace(stateFile)
	if stateFile == "" {
		add("state_file", "fail", "状态文件路径不能为空")
		return
	}
	info, err := os.Stat(stateFile)
	if err == nil {
		if info.IsDir() {
			add("state_file", "fail", "状态文件路径指向目录: "+stateFile)
			return
		}
		if info.Mode().Perm()&0o077 != 0 {
			add("state_file", "warn", fmt.Sprintf("权限为 %04o，建议收紧为 0600", info.Mode().Perm()))
			return
		}
		add("state_file", "ok", "已有状态文件权限正确")
		return
	}
	if !os.IsNotExist(err) {
		add("state_file", "fail", "读取状态文件失败: "+err.Error())
		return
	}
	parent := filepath.Dir(stateFile)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		add("state_file", "fail", "无法创建状态文件目录: "+err.Error())
		return
	}
	probe, err := os.CreateTemp(parent, ".beat-agent-doctor-*")
	if err != nil {
		add("state_file", "fail", "状态文件目录不可写: "+err.Error())
		return
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)
	add("state_file", "ok", "状态文件目录可写")
}
