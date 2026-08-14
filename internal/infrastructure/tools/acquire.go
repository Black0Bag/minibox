package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/tools"
)

// Acquirer B8 工具自动获取器（runx/ocx 2026 实证）。
// 安全设计（golang-security）：
//   - SHA-256 校验 + fail-closed（哈希不符拒绝运行，绝不运行未验证二进制）
//   - 原子安装（临时文件 + rename，避免半成品文件被误执行）
//   - 隔离 PATH（前置工具 bin + /usr/bin:/bin，不改系统环境变量）
//   - 断点续传 + 重试退避（网络不稳时也能可靠拉取）
type Acquirer struct {
	// Dir 工具存放目录（运行时文件夹，B8：不污染系统）。
	Dir string
	// HTTP 客户端。
	client *http.Client
}

// ToolSpec 工具定义（B8 清单式）。
type ToolSpec struct {
	// Name 工具名（注册名）。
	Name string `json:"name"`
	// URL 二进制下载地址。
	URL string `json:"url"`
	// SHA256 期望哈希（hex，小写）。空则拒绝下载（fail-closed）。
	SHA256 string `json:"sha256"`
	// Description 工具描述。
	Description string `json:"description"`
	// MaxSizeMB 下载大小上限（防 zip bomb/超限）。
	MaxSizeMB int64 `json:"max_size_mb"`
}

// AcquireResult 获取结果。
type AcquireResult struct {
	// Path 已安装的二进制绝对路径。
	Path string `json:"path"`
	// Installed 本次是否新安装（false=已缓存命中）。
	Installed bool `json:"installed"`
}

// NewAcquirer 创建工具获取器。
func NewAcquirer(dir string, timeout time.Duration) *Acquirer {
	return &Acquirer{
		Dir: dir,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// cachedPath 返回工具缓存的绝对路径（未必存在）。
func (a *Acquirer) cachedPath(spec ToolSpec) string {
	return filepath.Join(a.Dir, spec.Name)
}

// Reset 清空缓存目录（更新工具用）。
func (a *Acquirer) Reset() error {
	return os.RemoveAll(a.Dir)
}

// cacheExists 缓存是否存在且哈希匹配。
func (a *Acquirer) cacheExists(spec ToolSpec) (bool, string, error) {
	p := a.cachedPath(spec)
	return verifySHA256File(p, spec.SHA256)
}

// Acquire 获取工具：
//  1. 缓存命中（哈希匹配）→ 直接返回
//  2. 未命中 → 下载 → 校验 → 原子安装
//  3. 失败/哈希不符 → 返回错误（fail-closed）
func (a *Acquirer) Acquire(ctx context.Context, spec ToolSpec) (*AcquireResult, error) {
	if spec.Name == "" || spec.URL == "" {
		return nil, fmt.Errorf("工具 spec 不完整（name/url 必填）")
	}
	if spec.SHA256 == "" {
		// fail-closed：不校验哈希的二进制拒绝下载
		return nil, fmt.Errorf("工具 %s 缺少 SHA-256（fail-closed 拒绝下载）", spec.Name)
	}
	if spec.MaxSizeMB <= 0 {
		spec.MaxSizeMB = 100 // 默认 100MB 上限
	}

	// 命中缓存
	if ok, _, err := a.cacheExists(spec); err != nil {
		return nil, err
	} else if ok {
		return &AcquireResult{Path: a.cachedPath(spec), Installed: false}, nil
	}

	// 下载到临时文件（可断点续传）
	tmp, err := a.downloadWithRetry(ctx, spec)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tmp) }()

	// 哈希校验（fail-closed）
	ok, hash, err := verifySHA256File(tmp, spec.SHA256)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("工具 %s 哈希校验失败: 期望 %s 实际 %s（fail-closed 拒绝安装）",
			spec.Name, spec.SHA256, hash)
	}

	// 原子安装：rename 临时文件到最终路径
	final := a.cachedPath(spec)
	if err := atomicInstall(tmp, final); err != nil {
		return nil, err
	}

	return &AcquireResult{Path: final, Installed: true}, nil
}

// downloadWithRetry 带断点续传 + 重试退避地下载。
func (a *Acquirer) downloadWithRetry(ctx context.Context, spec ToolSpec) (string, error) {
	if err := os.MkdirAll(a.Dir, 0o750); err != nil {
		return "", fmt.Errorf("创建工具目录失败: %w", err)
	}

	tmp := filepath.Join(a.Dir, "."+spec.Name+".part")
	// 断点续传：从已有 .part 大小继续
	var offset int64
	if fi, err := os.Stat(tmp); err == nil {
		offset = fi.Size()
	}

	// 重试退避（最多 3 次：1s → 2s → 4s）
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(1<<attempt) * time.Second):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		err := a.downloadOnce(ctx, spec, tmp, offset)
		if err == nil {
			return tmp, nil
		}
		lastErr = err
		// 更新断点（续传后 .part 变大）
		if fi, err := os.Stat(tmp); err == nil {
			offset = fi.Size()
		}
	}
	return "", fmt.Errorf("工具 %s 下载失败（重试 3 次）: %w", spec.Name, lastErr)
}

// downloadOnce 单次下载（从 offset 断点续传）。
func (a *Acquirer) downloadOnce(ctx context.Context, spec ToolSpec, tmp string, offset int64) error {
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o700)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// 断点续传：服务器返回 206 Partially Content 时用 Append 已正确；
	// 若服务器不支持 Range 返回 200，需从头写
	if offset > 0 && resp.StatusCode == http.StatusOK {
		// 服务器忽略了 Range → 重置文件从头下载
		if err := f.Truncate(0); err != nil {
			return err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	// 大小上限（防超限）
	limit := spec.MaxSizeMB << 20
	n, err := io.Copy(f, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("下载超过大小上限 %d MB", spec.MaxSizeMB)
	}
	// 确认成功写入（sync 到磁盘）
	return f.Sync()
}

// verifySHA256File 校验文件哈希是否匹配。
// 返回 (匹配, 实际哈希, 错误)。文件不存在返回 (false, "", nil)。
func verifySHA256File(path, want string) (bool, string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "", nil
		}
		return false, "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, "", err
	}
	got := hex.EncodeToString(h.Sum(nil))
	return got == want, got, nil
}

// atomicInstall 原子安装：rename 临时文件到目标，并确保可执行。
func atomicInstall(tmp, final string) error {
	if err := os.Chmod(tmp, 0o700); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("原子安装失败: %w", err)
	}
	return nil
}

// b8Tool 把 Acquirer 暴露为一个工具（LLM 可直接请求下载工具）。
// 说明：不注册系统 PATH，仅下载到隔离目录供后续调用。
type b8Tool struct {
	acq *Acquirer
}

func (b *b8Tool) Name() string { return "acquire_tool" }
func (b *b8Tool) Description() string {
	return "按 B8 自动获取外部工具二进制（SHA-256 校验 + 隔离目录）。输入 {name, url, sha256, description?}。高风险。"
}
func (b *b8Tool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"name":{"type":"string","description":"工具名"},
			"url":{"type":"string","description":"二进制下载地址"},
			"sha256":{"type":"string","description":"期望 SHA-256 hex"},
			"description":{"type":"string"}
		},
		"required":["name","url","sha256"]
	}`)
}
func (b *b8Tool) Metadata() tools.Metadata {
	return tools.Metadata{
		OpenWorld:         true,
		MaxResultSize:     512,
		RiskTier:          "high",
		RequiresApproval:  true,
	}
}

func (b *b8Tool) Invoke(ctx context.Context, input json.RawMessage) (string, error) {
	var spec ToolSpec
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &spec); err != nil {
			return "", fmt.Errorf("参数解析失败: %w", err)
		}
	}
	res, err := b.acq.Acquire(ctx, spec)
	if err != nil {
		return "", err
	}
	if res.Installed {
		return fmt.Sprintf("已安装到 %s", res.Path), nil
	}
	return fmt.Sprintf("缓存命中: %s", res.Path), nil
}

var _ tools.Tool = (*b8Tool)(nil)

// NewAcquireTool 创建 B8 工具获取工具。
func NewAcquireTool(acq *Acquirer) tools.Tool {
	return &b8Tool{acq: acq}
}

// IsolatedRun 在隔离 PATH 下运行已获取的工具（runx 模式）。
// PATH = 工具目录 + /usr/bin:/bin（前置工具 bin，不改系统环境变量，B8）。
// 安全（golang-security）：exec 独立参数传参，绝不拼 shell。
func IsolatedRun(ctx context.Context, toolDir, bin string, args []string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	path := bin
	if !filepath.IsAbs(path) {
		path = filepath.Join(toolDir, bin)
	}

	cmd := exec.CommandContext(ctx, path, args...)
	// 前置工具 bin，后跟系统基础目录（隔离 PATH）
	cmd.Env = append(os.Environ(), "PATH="+toolDir+":"+isolationPath())
	cmd.Env = append(cmd.Env, "PYTHONNOUSERSITE=1", "PYTHONPATH=")

	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("工具 %s 执行超时（%s）", bin, timeout)
	}
	if err != nil {
		return string(out), fmt.Errorf("工具 %s 执行失败: %v", bin, err)
	}
	return string(out), nil
}