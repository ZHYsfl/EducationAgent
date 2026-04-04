package pptx

import (
	"fmt"
	"io"
	"os"

	"github.com/kenny-not-dead/gopptx"
)

// RoundTrip 打开 srcPPTX 并写入 dstPPTX（gopptx 解析再序列化）。
// 用于「方案 B」基线：验证 Go 栈可读写与 Python 相同的模板文件。
// 注意：输出字节流未必与 python-pptx 另存完全一致（ZIP 顺序等），视觉一致性需另行对比。
func RoundTrip(srcPPTX, dstPPTX string) error {
	f, err := gopptx.OpenFile(srcPPTX)
	if err != nil {
		return fmt.Errorf("gopptx.OpenFile: %w", err)
	}
	defer f.Close()
	if err := f.SaveAs(dstPPTX); err != nil {
		return fmt.Errorf("SaveAs: %w", err)
	}
	return nil
}

// RoundTripToTemp 将 src 往返写入 os.CreateTemp 生成的路径（调用方负责删除）。
func RoundTripToTemp(srcPPTX, pattern string) (path string, err error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	path = f.Name()
	_ = f.Close()
	if err := RoundTrip(srcPPTX, path); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

// CopyFile 辅助：将文件复制到目标路径（用于测试准备）。
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
