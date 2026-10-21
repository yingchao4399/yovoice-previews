package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// localService 只管理本地 Go 服务的生命周期与调用；业务状态由服务统一持有，与 Windows、macOS 宿主一致。
type localService struct {
	root   string
	token  string
	client *http.Client
	// failed 在服务意外退出时调用（非主线程）。
	failed func(error)

	mu       sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	done     chan struct{}
	origin   string
	stopping bool
}

func newLocalService(root string) *localService {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	// 本机调用不能走系统代理，否则代理会拒绝或记录 127.0.0.1 的会话凭证。
	transport := &http.Transport{Proxy: nil}
	return &localService{root: root, token: hex.EncodeToString(b), client: &http.Client{Transport: transport}}
}

func (s *localService) cookieName() string { return "vw-" + s.token[:12] }

func (s *localService) Origin() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.origin
}

// installDir 是宿主可执行文件所在目录，服务、界面与内核都放在它旁边。
func installDir() string {
	executable, err := os.Executable()
	if err == nil {
		if resolved, e := filepath.EvalSymlinks(executable); e == nil {
			executable = resolved
		}
	}
	return filepath.Dir(executable)
}

func serviceExecutable() string { return filepath.Join(installDir(), "service", "yovoice-service") }

// runService 同步运行服务的辅助命令并返回标准输出；退出码非零时以服务的错误信息返回。
func runService(args ...string) ([]byte, error) {
	executable := serviceExecutable()
	if _, err := os.Stat(executable); err != nil {
		return nil, errors.New(T("本地服务缺失，请重新安装应用。", "The local service is missing. Reinstall the app."))
	}
	var stderr bytes.Buffer
	cmd := exec.Command(executable, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return nil, errors.New(detail)
		}
		return nil, err
	}
	return out, nil
}

// configuredDataRoot 返回“设置 › 存储”选择的数据目录；配置只由服务解析（--data-root），
// 损坏时返回错误由调用方提示后退出，不静默回退到默认目录。
func configuredDataRoot() (string, error) {
	out, err := runService("--data-root")
	if err != nil {
		return "", err
	}
	var config struct {
		DataPath string `json:"dataPath"`
	}
	if err = json.Unmarshal(out, &config); err != nil {
		return "", err
	}
	if config.DataPath == "" {
		return "", nil
	}
	return filepath.Abs(config.DataPath)
}

func (s *localService) Start() error {
	s.mu.Lock()
	if s.cmd != nil && s.done != nil {
		select {
		case <-s.done:
		default:
			s.mu.Unlock()
			return nil
		}
	}
	s.origin = ""
	s.stopping = false
	s.mu.Unlock()
	hostLog(s.root, "service.start_requested", nil)
	executable := serviceExecutable()
	if _, err := os.Stat(executable); err != nil {
		return errors.New(T("本地服务缺失，请重新安装应用。", "The local service is missing. Reinstall the app."))
	}
	if os.Getenv("WORKBENCH_DATA") == "" {
		if _, err := runService("--prepare-data"); err != nil {
			return fmt.Errorf("%s%w", T("数据目录迁移失败：", "Data folder migration failed: "), err)
		}
	}
	if err := os.MkdirAll(filepath.Join(s.root, "logs"), 0700); err != nil {
		return err
	}
	// 服务日志追加写入同一文件，超过 5MB 才滚动为 .1；崩溃后重启时上一轮的出错原因仍在文件中。
	logPath := filepath.Join(s.root, "logs", "host-service.log")
	if info, err := os.Stat(logPath); err == nil && info.Size() >= 5*1024*1024 {
		_ = os.Rename(logPath, logPath+".1")
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	// 子进程继承日志文件后宿主侧随即关闭，避免每次重启服务都泄漏一个句柄。
	defer logFile.Close()
	_, _ = fmt.Fprintf(logFile, "\n===== %s service =====\n", time.Now().Format(time.RFC3339))
	base := installDir()
	cmd := exec.Command(executable)
	cmd.Env = append(os.Environ(),
		"WORKBENCH_DATA="+s.root,
		"WORKBENCH_WEB="+filepath.Join(base, "web"),
		"WORKBENCH_TOKEN="+s.token,
		"WORKBENCH_ENGINE="+filepath.Join(base, "engine", "audiocpp_server"),
	)
	cmd.Stderr = logFile
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("%s%w", T("无法启动本地服务：", "Could not start the local service: "), err)
	}
	done := make(chan struct{})
	s.mu.Lock()
	s.cmd, s.stdin, s.done = cmd, stdin, done
	s.mu.Unlock()
	go func() {
		err := cmd.Wait()
		close(done)
		s.mu.Lock()
		expected := s.stopping || s.cmd != cmd
		s.mu.Unlock()
		hostLog(s.root, "service.exited", err)
		if !expected && s.failed != nil {
			code := cmd.ProcessState.ExitCode()
			s.failed(fmt.Errorf("%s", T(fmt.Sprintf("本地服务意外停止（%d），诊断记录保存在日志目录。", code), fmt.Sprintf("The local service stopped unexpectedly (%d). Diagnostics are in the logs folder.", code))))
		}
	}()
	// 服务只在首行输出启动地址，其余日志写入文件；此后继续读空标准输出，避免管道写满阻塞服务。
	lines := make(chan string, 1)
	go func() {
		reader := bufio.NewReader(stdout)
		line, _ := reader.ReadString('\n')
		lines <- strings.TrimSpace(line)
		_, _ = io.Copy(io.Discard, reader)
	}()
	var address string
	select {
	case address = <-lines:
	case <-time.After(30 * time.Second):
	}
	parsed, perr := url.Parse(address)
	if address == "" || perr != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" {
		// 服务没在限时内给出地址：结束它再报告，避免残留进程让重试误判为已启动。
		s.terminate(10 * time.Second)
		return errors.New(T("本地服务未返回有效地址，请检查日志。", "The local service did not return a valid address. Check the logs."))
	}
	s.mu.Lock()
	s.origin = parsed.Scheme + "://" + parsed.Host
	s.mu.Unlock()
	hostLog(s.root, "service.ready", nil)
	return nil
}

// callError 保留服务返回的原始错误，交给网页按当前语言显示。
type callError struct{ wire json.RawMessage }

func (e *callError) Code() string {
	var wire struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(e.wire, &wire)
	return wire.Code
}

func (e *callError) Error() string {
	var text string
	if json.Unmarshal(e.wire, &text) == nil {
		return text
	}
	var wire struct {
		Code   string `json:"code"`
		Params struct {
			Detail string `json:"detail"`
			Path   string `json:"path"`
		} `json:"params"`
	}
	if json.Unmarshal(e.wire, &wire) != nil || wire.Code == "" {
		return T("本地服务请求失败。", "The local service request failed.")
	}
	switch wire.Code {
	case "@yovoice.error.diskFull":
		return T("磁盘空间不足。", "The disk is full.")
	case "@yovoice.error.dataRootMissing":
		return T("找不到数据目录 ", "The data folder can't be found: ") + wire.Params.Path
	}
	if wire.Params.Detail != "" {
		return wire.Code + "\n" + wire.Params.Detail
	}
	return wire.Code
}

const storageRestartRequired = "@yovoice.error.storageRestartRequired"

func (s *localService) request(ctx context.Context, path string, body []byte) (*http.Response, error) {
	origin := s.Origin()
	if origin == "" {
		return nil, errors.New(T("本地服务尚未就绪。", "The local service isn't ready."))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/"+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", s.cookieName()+"="+s.token)
	return s.client.Do(req)
}

// Call 调用服务方法；普通请求 120 秒上限，迁移数据目录等长调用由调用方放宽。
func (s *localService) Call(method string, data any, timeout time.Duration) (json.RawMessage, error) {
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	var raw json.RawMessage
	switch value := data.(type) {
	case json.RawMessage:
		raw = value
	default:
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(value); err != nil {
			return nil, err
		}
		raw = bytes.TrimSpace(buffer.Bytes())
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	body, err := json.Marshal(struct {
		ID     string          `json:"id"`
		Method string          `json:"method"`
		Data   json.RawMessage `json:"data"`
	}{newID(), method, raw})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	response, err := s.request(ctx, "api/call", body)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s (%d)", T("本地服务请求失败。", "The local service request failed."), response.StatusCode)
	}
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err = json.NewDecoder(response.Body).Decode(&reply); err != nil {
		return nil, err
	}
	if len(reply.Error) > 0 && string(reply.Error) != "null" {
		return nil, &callError{reply.Error}
	}
	if len(reply.Result) == 0 {
		reply.Result = json.RawMessage("null")
	}
	return reply.Result, nil
}

// Running 判断是否有生成或模型下载正在进行；查询失败时视为没有，后续保存失败会提供备份后退出。
func (s *localService) Running() bool {
	result, err := s.Call("state.get", struct{}{}, 0)
	if err != nil {
		hostLog(s.root, "exit.state_failed", err)
		return false
	}
	var reply struct {
		State map[string]json.RawMessage `json:"state"`
	}
	if json.Unmarshal(result, &reply) != nil {
		return false
	}
	for _, key := range []string{"activity", "modelActivity"} {
		var activity struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(reply.State[key], &activity) == nil && activity.Status == "running" {
			return true
		}
	}
	return false
}

func (s *localService) Alive() bool {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done == nil {
		return false
	}
	select {
	case <-done:
		return false
	default:
		return true
	}
}

// Shutdown 请求服务保存并退出；服务已接受关停后不能再让退出失败。
func (s *localService) Shutdown() error {
	hostLog(s.root, "service.shutdown_requested", nil)
	if !s.Alive() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	response, err := s.request(ctx, "shutdown", []byte("{}"))
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s (%d)", T("本地服务未能停止。", "The local service didn't stop."), response.StatusCode)
	}
	s.terminate(10 * time.Second)
	hostLog(s.root, "service.shutdown_completed", nil)
	return nil
}

// Stop 用于正常关停失败时：先关闭标准输入让服务自行退出，超时则结束进程。
func (s *localService) Stop() { s.terminate(5 * time.Second) }

func (s *localService) terminate(wait time.Duration) {
	s.mu.Lock()
	s.stopping = true
	cmd, stdin, done := s.cmd, s.stdin, s.done
	s.mu.Unlock()
	if cmd == nil {
		return
	}
	_ = stdin.Close()
	select {
	case <-done:
	case <-time.After(wait):
		hostLog(s.root, "service.shutdown_timeout", nil)
		_ = cmd.Process.Kill()
		<-done
	}
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
