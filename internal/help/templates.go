// Package help provides built-in repository help documentation and dynamic client configuration rendering.
package help

import (
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

// TemplateInfo represents metadata about an available help template.
type TemplateInfo struct {
	ID       string              `json:"id"`
	Title    string              `json:"title"`
	Summary  string              `json:"summary"`
	Type     string              `json:"type"`
	Version  int                 `json:"version"`
	Variants []model.HelpVariant `json:"variants,omitempty"`
	Formats  []model.HelpFormat  `json:"formats,omitempty"`
}

// Built-in help markdown templates.
var templates = map[string]string{
	"builtin://help/debian.md": `## Debian 软件仓库配置说明

### 1. 软件源地址
当前仓库公开地址：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 自动替换命令 (推荐)
执行以下命令备份并替换官方源为当前镜像站：
` + "```" + `bash
sudo sed -i.bak 's|http://deb.debian.org/debian|{{REPOSITORY_URL}}|g' /etc/apt/sources.list /etc/apt/sources.list.d/*.sources 2>/dev/null || true
sudo apt-get update
` + "```" + `

### 3. 完整配置示例

{{CONFIG_BLOCK}}

配置完成后执行更新：
` + "```" + `bash
sudo apt-get update
` + "```" + `
`,

	"builtin://help/debian-security.md": `## Debian Security 软件源配置说明

### 1. 软件源地址
当前仓库公开地址：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 自动替换命令
` + "```" + `bash
sudo sed -i.bak 's|http://security.debian.org/debian-security|{{REPOSITORY_URL}}|g' /etc/apt/sources.list /etc/apt/sources.list.d/*.sources 2>/dev/null || true
sudo apt-get update
` + "```" + `

### 3. 完整配置示例

{{CONFIG_BLOCK}}
`,

	"builtin://help/ubuntu.md": `## Ubuntu 软件仓库配置说明

### 1. 软件源地址
当前仓库公开地址：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 自动替换命令 (推荐)
执行以下命令备份并替换 Ubuntu 官方源：
` + "```" + `bash
sudo sed -i.bak 's|http://archive.ubuntu.com/ubuntu|{{REPOSITORY_URL}}|g; s|http://security.ubuntu.com/ubuntu|{{REPOSITORY_URL}}|g' /etc/apt/sources.list /etc/apt/sources.list.d/*.sources 2>/dev/null || true
sudo apt-get update
` + "```" + `

### 3. 完整配置示例

{{CONFIG_BLOCK}}
`,

	"builtin://help/rocky.md": `## Rocky Linux 软件仓库配置说明

### 1. 软件源地址
当前仓库公开地址：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 自动替换命令
` + "```" + `bash
sudo sed -e 's|^mirrorlist=|#mirrorlist=|g' \
     -e 's|^#baseurl=http://dl.rockylinux.org/$contentdir|baseurl={{REPOSITORY_URL}}|g' \
     -i.bak \
     /etc/yum.repos.d/rocky*.repo
sudo dnf makecache
` + "```" + `

### 3. 配置示例

{{CONFIG_BLOCK}}
`,

	"builtin://help/almalinux.md": `## AlmaLinux 软件仓库配置说明

### 1. 软件源地址
当前仓库公开地址：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 自动替换命令
` + "```" + `bash
sudo sed -e 's|^mirrorlist=|#mirrorlist=|g' \
     -e 's|^# baseurl=https://repo.almalinux.org/almalinux|baseurl={{REPOSITORY_URL}}|g' \
     -i.bak \
     /etc/yum.repos.d/almalinux*.repo
sudo dnf makecache
` + "```" + `
`,

	"builtin://help/fedora.md": `## Fedora 软件仓库配置说明

### 1. 软件源地址
当前仓库公开地址：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 自动替换命令
` + "```" + `bash
sudo sed -e 's|^metalink=|#metalink=|g' \
     -e 's|^#baseurl=http://download.example/pub/fedora/linux|baseurl={{REPOSITORY_URL}}|g' \
     -i.bak \
     /etc/yum.repos.d/fedora*.repo
sudo dnf makecache
` + "```" + `
`,

	"builtin://help/epel.md": `## EPEL 额外软件包仓库配置说明

### 1. 软件源地址
当前仓库公开地址：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 自动替换命令
` + "```" + `bash
sudo sed -e 's|^metalink=|#metalink=|g' \
     -e 's|^#baseurl=https://download.example/pub/epel|baseurl={{REPOSITORY_URL}}|g' \
     -i.bak \
     /etc/yum.repos.d/epel*.repo
sudo dnf makecache
` + "```" + `
`,

	"builtin://help/alpine.md": `## Alpine Linux 软件仓库配置说明

### 1. 软件源地址
当前仓库公开地址：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 自动替换命令
` + "```" + `bash
sudo sed -i.bak 's|https://dl-cdn.alpinelinux.org/alpine|{{REPOSITORY_URL}}|g' /etc/apk/repositories
apk update
` + "```" + `

### 3. 配置示例

{{CONFIG_BLOCK}}
`,

	"builtin://help/pypi.md": `## PyPI 镜像源配置说明

### 1. 镜像源地址
当前 PyPI 镜像源公开地址：
` + "```" + `text
{{REPOSITORY_URL}}simple
` + "```" + `

### 2. 临时使用
在 pip 命令中通过 ` + "`-i`" + ` 参数指定镜像源：
` + "```" + `bash
pip install -i {{REPOSITORY_URL}}simple some-package
` + "```" + `

### 3. 设为默认源
执行以下命令配置为全局默认：
` + "```" + `bash
pip config set global.index-url {{REPOSITORY_URL}}simple
` + "```" + `
`,

	"builtin://help/npm.md": `## npm 镜像源配置说明

### 1. 镜像源地址
当前 npm 镜像源公开地址：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 临时使用
` + "```" + `bash
npm --registry={{REPOSITORY_URL}} install some-package
` + "```" + `

### 3. 设为默认源
` + "```" + `bash
npm config set registry {{REPOSITORY_URL}}
` + "```" + `
`,

	"builtin://help/docker-ce.md": `## Docker CE 社区版安装源配置说明

### 1. 软件源地址
当前仓库公开地址：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 自动替换命令
` + "```" + `bash
sudo sed -i.bak 's|https://download.docker.com/linux|{{REPOSITORY_URL}}|g' /etc/apt/sources.list.d/docker*.list /etc/apt/sources.list.d/docker*.sources /etc/yum.repos.d/docker*.repo 2>/dev/null || true
` + "```" + `
`,

	"builtin://help/openwrt.md": `## OpenWrt / OPKG 软件仓库配置说明

### 1. 软件源地址
当前仓库公开地址：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 自动替换命令
` + "```" + `bash
sed -i.bak 's|https://downloads.openwrt.org|{{REPOSITORY_URL}}|g' /etc/opkg/distfeeds.conf
opkg update
` + "```" + `
`,

	"builtin://help/ubuntu-iso.md": `## Ubuntu Releases 官方 ISO 镜像下载

### 1. 镜像目录地址
当前镜像站公开访问路径：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 常用版本快捷下载 (推荐使用多线程断点续传)

#### Aria2 (高速 16 线程下载)
` + "```" + `bash
aria2c -x 16 -s 16 -k 1M -c {{REPOSITORY_URL}}noble/ubuntu-24.04-desktop-amd64.iso
` + "```" + `

#### Wget (断点续传)
` + "```" + `bash
wget -c {{REPOSITORY_URL}}noble/ubuntu-24.04-desktop-amd64.iso
` + "```" + `

#### Curl
` + "```" + `bash
curl -C - -O -L {{REPOSITORY_URL}}noble/ubuntu-24.04-desktop-amd64.iso
` + "```" + `

### 3. SHA256 完整性校验
` + "```" + `bash
curl -sSL {{REPOSITORY_URL}}noble/SHA256SUMS | sha256sum -c --ignore-missing
` + "```" + `
`,

	"builtin://help/debian-cd.md": `## Debian CD / Live ISO 镜像下载

### 1. 镜像目录地址
当前镜像站公开访问路径：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 常用安装盘下载

#### Netinst 网络安装盘 (约 700MB)
` + "```" + `bash
aria2c -x 16 -s 16 -k 1M -c {{REPOSITORY_URL}}current/amd64/iso-cd/debian-12.8.0-amd64-netinst.iso
` + "```" + `

#### DVD 离线完整安装盘 (约 4.4GB)
` + "```" + `bash
aria2c -x 16 -s 16 -k 1M -c {{REPOSITORY_URL}}current/amd64/iso-dvd/debian-12.8.0-amd64-DVD-1.iso
` + "```" + `

### 3. SHA256 校验
` + "```" + `bash
curl -sSL {{REPOSITORY_URL}}current/amd64/iso-cd/SHA256SUMS | sha256sum -c --ignore-missing
` + "```" + `
`,

	"builtin://help/rocky-iso.md": `## Rocky Linux 官方安装 ISO 镜像下载

### 1. 镜像目录地址
当前镜像站公开访问路径：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 常用 ISO 镜像下载

#### Rocky Linux 9 x86_64 DVD ISO (完整离线安装)
` + "```" + `bash
aria2c -x 16 -s 16 -k 1M -c {{REPOSITORY_URL}}9/isos/x86_64/Rocky-9-latest-x86_64-dvd.iso
` + "```" + `

#### Rocky Linux 9 x86_64 Minimal ISO (最小化安装)
` + "```" + `bash
aria2c -x 16 -s 16 -k 1M -c {{REPOSITORY_URL}}9/isos/x86_64/Rocky-9-latest-x86_64-minimal.iso
` + "```" + `
`,

	"builtin://help/arch-iso.md": `## Arch Linux 官方安装镜像下载

### 1. 镜像目录地址
当前镜像站公开访问路径：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 最新 ISO 镜像下载
` + "```" + `bash
aria2c -x 16 -s 16 -k 1M -c {{REPOSITORY_URL}}latest/archlinux-x86_64.iso
` + "```" + `

### 3. SHA256 校验
` + "```" + `bash
curl -sSL {{REPOSITORY_URL}}latest/sha256sums.txt | sha256sum -c --ignore-missing
` + "```" + `
`,

	"builtin://help/iso.md": `## 系统镜像与 ISO 资源下载说明

### 1. 镜像文件目录
当前镜像站公开访问路径：
` + "```" + `text
{{REPOSITORY_URL}}
` + "```" + `

### 2. 多线程与断点续传下载工具

#### Aria2 (推荐，16 并发分块下载)
` + "```" + `bash
aria2c -x 16 -s 16 -k 1M -c {{REPOSITORY_URL}}path/to/image.iso
` + "```" + `

#### Wget (断点续传)
` + "```" + `bash
wget -c {{REPOSITORY_URL}}path/to/image.iso
` + "```" + `

#### Curl (断点续传)
` + "```" + `bash
curl -C - -O -L {{REPOSITORY_URL}}path/to/image.iso
` + "```" + `
`,
}

// GetTemplate returns the raw template text by template ID.
func GetTemplate(id string) (string, bool) {
	if tmpl, ok := templates[id]; ok {
		return tmpl, true
	}
	normalized := id
	if !strings.HasPrefix(normalized, "builtin://help/") {
		normalized = "builtin://help/" + strings.TrimPrefix(normalized, "/")
	}
	if !strings.HasSuffix(normalized, ".md") {
		normalized = normalized + ".md"
	}
	tmpl, ok := templates[normalized]
	return tmpl, ok
}

// ListTemplates returns a list of available built-in templates.
func ListTemplates() []TemplateInfo {
	return []TemplateInfo{
		{ID: "builtin://help/debian.md", Title: "Debian", Summary: "Debian 软件包仓库使用说明", Type: "apt", Version: 1},
		{ID: "builtin://help/debian-security.md", Title: "Debian Security", Summary: "Debian Security 安全更新源使用说明", Type: "apt", Version: 1},
		{ID: "builtin://help/ubuntu.md", Title: "Ubuntu", Summary: "Ubuntu 软件包仓库使用说明", Type: "apt", Version: 1},
		{ID: "builtin://help/rocky.md", Title: "Rocky Linux", Summary: "Rocky Linux 软件包仓库使用说明", Type: "rpm", Version: 1},
		{ID: "builtin://help/almalinux.md", Title: "AlmaLinux", Summary: "AlmaLinux 软件包仓库使用说明", Type: "rpm", Version: 1},
		{ID: "builtin://help/fedora.md", Title: "Fedora", Summary: "Fedora 软件包仓库使用说明", Type: "rpm", Version: 1},
		{ID: "builtin://help/epel.md", Title: "EPEL", Summary: "EPEL 额外软件包仓库使用说明", Type: "rpm", Version: 1},
		{ID: "builtin://help/alpine.md", Title: "Alpine", Summary: "Alpine Linux apk 软件包仓库使用说明", Type: "apk", Version: 1},
		{ID: "builtin://help/pypi.md", Title: "PyPI", Summary: "PyPI Python 软件包索引使用说明", Type: "pypi", Version: 1},
		{ID: "builtin://help/npm.md", Title: "npm", Summary: "npm JavaScript 软件包注册表使用说明", Type: "npm", Version: 1},
		{ID: "builtin://help/docker-ce.md", Title: "Docker CE", Summary: "Docker CE 社区版安装源使用说明", Type: "apt", Version: 1},
		{ID: "builtin://help/openwrt.md", Title: "OpenWrt", Summary: "OpenWrt opkg 软件包仓库使用说明", Type: "opkg", Version: 1},
		{ID: "builtin://help/ubuntu-iso.md", Title: "Ubuntu Releases (ISO)", Summary: "Ubuntu 官方 ISO 系统镜像下载指南", Type: "iso", Version: 1},
		{ID: "builtin://help/debian-cd.md", Title: "Debian CD / ISO", Summary: "Debian 官方安装盘与 Live ISO 镜像下载指南", Type: "iso", Version: 1},
		{ID: "builtin://help/rocky-iso.md", Title: "Rocky Linux ISO", Summary: "Rocky Linux 官方系统安装镜像下载指南", Type: "iso", Version: 1},
		{ID: "builtin://help/arch-iso.md", Title: "Arch Linux ISO", Summary: "Arch Linux 官方安装镜像下载指南", Type: "iso", Version: 1},
		{ID: "builtin://help/iso.md", Title: "Generic ISO / Images", Summary: "通用操作系统镜像与 ISO 下载说明", Type: "iso", Version: 1},
	}
}
