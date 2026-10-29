package core

import "fmt"

// CrashError reports a core process that died on a Windows exception code
// instead of answering. On Windows the interesting ones are almost always an
// environment problem, not a bug in the core: a security product injecting into
// or blocking the child, a damaged executable, or a CPU the build cannot run on.
type CrashError struct {
	Code uint32
}

func (e *CrashError) Error() string {
	return fmt.Sprintf("内核进程崩溃（%s）", CrashCodeName(e.Code))
}

// CrashHint is the advice that goes with a crash code: what to try, in the order
// that actually fixes it.
func (e *CrashError) Hint() string {
	switch e.Code {
	case 0xC0000005:
		return "通常是安全软件拦截/注入，或内核文件损坏。请把 XEHN 目录加入杀毒白名单后重试；仍然失败就在设置页重新下载内核。"
	case 0xC000001D:
		return "CPU 不支持该内核使用的指令集。请在设置页重新下载内核（新版会按 CPU 能力构建）。"
	case 0xC0000135, 0xC0000142:
		return "缺少或无法加载系统 DLL。请安装最新的 Visual C++ 运行库/系统更新后重试。"
	case 0xC0000409:
		return "触发栈保护，常见于安全软件的注入或拦截。请把 XEHN 目录加入白名单后重试。"
	}
	return "内核进程启动即退出。请检查安全软件拦截记录，或重新下载内核。"
}

// CrashCodeName renders a Windows exception code for a log line.
func CrashCodeName(code uint32) string {
	switch code {
	case 0xC0000005:
		return "0xC0000005 访问冲突"
	case 0xC000001D:
		return "0xC000001D 非法指令"
	case 0xC0000135:
		return "0xC0000135 缺少 DLL"
	case 0xC0000142:
		return "0xC0000142 DLL 初始化失败"
	case 0xC0000409:
		return "0xC0000409 栈保护"
	}
	return fmt.Sprintf("0x%08X", code)
}
