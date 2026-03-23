package renderhtml

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// WrapFullHTML 与 Python execute_render_job 中外壳一致（1280×720、微软雅黑）。
func WrapFullHTML(inner string) string {
	return `<!DOCTYPE html><html><head><meta charset="utf-8"/>
<style>body{margin:0;padding:28px;box-sizing:border-box;width:1280px;height:720px;
font-family:'Microsoft YaHei','Segoe UI',sans-serif;background:#fff;}
</style></head><body>` + inner + `</body></html>`
}

func fileURLFromPath(abs string) string {
	abs = filepath.Clean(abs)
	abs = filepath.ToSlash(abs)
	if runtime.GOOS == "windows" && len(abs) >= 2 && abs[1] == ':' {
		return "file:///" + abs
	}
	if !strings.HasPrefix(abs, "/") {
		abs = "/" + abs
	}
	return "file://" + abs
}

// WriteSlideJPEG 将 innerHTML 渲染为 taskDir/renders/slide_XXXX.jpg（JPEG quality 88，视口 1280×720）。
func WriteSlideJPEG(ctx context.Context, taskDir string, slideIndex int, innerHTML string, execPath string) error {
	renders := filepath.Join(taskDir, "renders")
	if err := os.MkdirAll(renders, 0o755); err != nil {
		return err
	}
	out := filepath.Join(renders, fmt.Sprintf("slide_%04d.jpg", slideIndex))
	full := WrapFullHTML(innerHTML)

	tmp, err := os.CreateTemp(renders, "render-*.html")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(full); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	defer func() { _ = os.Remove(tmpPath) }()

	absHTML, err := filepath.Abs(tmpPath)
	if err != nil {
		return err
	}
	fileURL := fileURLFromPath(absHTML)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
	)
	execPath = strings.TrimSpace(execPath)
	if execPath != "" {
		opts = append(opts, chromedp.ExecPath(execPath))
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()
	tctx, cancelTimeout := context.WithTimeout(browserCtx, 90*time.Second)
	defer cancelTimeout()

	var buf []byte
	if err := chromedp.Run(tctx,
		chromedp.EmulateViewport(1280, 720),
		chromedp.Navigate(fileURL),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.CaptureScreenshot(&buf),
	); err != nil {
		return fmt.Errorf("chromedp: %w", err)
	}
	if len(buf) == 0 {
		return fmt.Errorf("预览图未生成")
	}
	img, _, err := image.Decode(bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("decode screenshot: %w", err)
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 88}); err != nil {
		return err
	}
	return nil
}
