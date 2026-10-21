//go:build !windows

package backends

// registryDSHDesktopDirs 仅在 Windows 上可用；其他平台由显式路径/环境变量探测。
func registryDSHDesktopDirs() []string { return nil }
