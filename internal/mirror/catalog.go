package mirror

import "strings"

// Tier is the order in which a built-in mirror is tried.
type Tier int

const (
	// TierPrimary mirrors are probed first, in parallel.
	TierPrimary Tier = iota
	// TierBackup mirrors are probed only after every primary mirror failed.
	TierBackup
)

// Site is a registry that serves Docker Hub repositories at the same path.
type Site struct {
	Name  string // short name accepted by -m
	Title string
	Host  string
	Tier  Tier
}

// Builtin is the Docker Hub mirror list. Paths are the upstream repository
// path, so nginx is /library/nginx on every one of these hosts.
func Builtin() []Site {
	return []Site{
		{Name: "1ms", Title: "毫秒镜像", Host: "docker.1ms.run", Tier: TierPrimary},
		{Name: "xuanyuan", Title: "轩辕免费镜像", Host: "docker.xuanyuan.me", Tier: TierPrimary},
		{Name: "daocloud", Title: "DaoCloud", Host: "docker.m.daocloud.io", Tier: TierBackup},
		{Name: "hubfast", Title: "HubFast", Host: "free.hubfast.cn", Tier: TierBackup},
		{Name: "dockerproxy", Title: "Docker Proxy", Host: "dockerproxy.net", Tier: TierBackup},
	}
}

// ByTier returns primary sites, then backup sites.
func ByTier() [][]Site {
	var primary, backup []Site
	for _, site := range Builtin() {
		if site.Tier == TierPrimary {
			primary = append(primary, site)
		} else {
			backup = append(backup, site)
		}
	}
	return [][]Site{primary, backup}
}

// Expand replaces a built-in short name with its host. A full address is
// returned unchanged apart from a trimmed scheme.
func Expand(spec string) string {
	raw := strings.TrimSpace(spec)
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.Trim(raw, "/")
	host, prefix, _ := strings.Cut(raw, "/")
	for _, site := range Builtin() {
		if strings.EqualFold(host, site.Name) || strings.EqualFold(host, site.Host) {
			if prefix == "" {
				return site.Host
			}
			return site.Host + "/" + prefix
		}
	}
	return raw
}

// IsDockerHub reports the registries that docker pull treats as Docker Hub.
func IsDockerHub(registry string) bool {
	switch strings.ToLower(registry) {
	case "docker.io", "index.docker.io", "registry-1.docker.io":
		return true
	default:
		return false
	}
}

// Lookup finds a built-in site by short name or host.
func Lookup(hostOrName string) (Site, bool) {
	key := strings.ToLower(strings.TrimSpace(hostOrName))
	key = strings.TrimPrefix(key, "https://")
	key = strings.TrimPrefix(key, "http://")
	host, _, _ := strings.Cut(strings.Trim(key, "/"), "/")
	for _, site := range Builtin() {
		if host == site.Name || host == site.Host {
			return site, true
		}
	}
	return Site{}, false
}
