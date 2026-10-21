//go:build windows

package backends

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// registryDSHDesktopDirs 从卸载注册表读取 DeepSeek Harness 的 InstallLocation。
// 桌面客户端默认安装位置可被用户改到任意盘，注册表是最可靠的来源。
func registryDSHDesktopDirs() []string {
	type root struct {
		key  registry.Key
		path string
	}
	roots := []root{
		{registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
	}
	var out []string
	for _, r := range roots {
		k, err := registry.OpenKey(r.key, r.path, registry.READ)
		if err != nil {
			continue
		}
		names, _ := k.ReadSubKeyNames(-1)
		for _, name := range names {
			sk, err := registry.OpenKey(k, name, registry.READ)
			if err != nil {
				continue
			}
			display, _, _ := sk.GetStringValue("DisplayName")
			if strings.Contains(display, "DeepSeek Harness") {
				if loc, _, err := sk.GetStringValue("InstallLocation"); err == nil {
					if loc = strings.Trim(strings.TrimSpace(loc), `"`); loc != "" {
						out = append(out, loc)
					}
				}
			}
			_ = sk.Close()
		}
		_ = k.Close()
	}
	return out
}
