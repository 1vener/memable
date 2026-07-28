// 包 errx：统一错误构造与包装，便于后续日志/响应层透传。
// 代码注释使用中文。
package errx

import "fmt"

// Wrapf 包装错误并附加上下文。
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(format+": %w", append(args, err)...)
}

// Newf 新建带格式的错误。
func Newf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
