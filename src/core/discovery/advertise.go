// Package discovery 在 HTTPS 服务实际监听的局域网接口发布 MusicFS 服务。
package discovery

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/grandcat/zeroconf"
)

const ServiceType = "_musicfs._tcp"

// Advertise 返回用于发送服务离线通知和关闭 mDNS 套接字的函数。回环监听或关闭发现时返回 nil。
func Advertise(c config.Config, listener *net.TCPAddr, instanceID string) (func(), error) {
	if !c.Discovery.Enabled || c.TLSCert == "" || listener.IP.IsLoopback() {
		return nil, nil
	}
	origin := c.DiscoveryOrigin(listener.Port)
	if origin == "" {
		return nil, errors.New("configure a reachable HTTPS allowedOrigins entry on the listener port for LAN discovery")
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var usable []net.Interface
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 || iface.Flags&net.FlagLoopback != 0 ||
			(listener.Zone != "" && listener.Zone != iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			network, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := network.IP
			if ip.IsLoopback() || !(ip.IsGlobalUnicast() || ip.IsLinkLocalUnicast()) {
				continue
			}
			if !listener.IP.IsUnspecified() && !listener.IP.Equal(ip) {
				continue
			}
			if listener.IP.To4() != nil && ip.To4() == nil {
				continue
			}
			usable = append(usable, iface)
			break
		}
	}
	if len(usable) == 0 {
		return nil, errors.New("no multicast interface matches the HTTPS listener")
	}

	// 使用服务专属的本地名称，避免和系统已有的 .local 主机记录冲突。
	serviceHost := fmt.Sprintf("musicfs-%s", instanceID)
	txt := []string{"api=1", "scheme=https", "origin=" + origin}
	// 使用管理员选定的 Origin 主机和端口作为人可辨认的显示名，TCP 端口始终来自实际监听器。
	name := fmt.Sprintf("MusicFS-%s", instanceID)
	if u, err := url.Parse(origin); err == nil && u.Host != "" {
		label := strings.ReplaceAll(u.Host, ".", "-")
		if len(label) <= 54 && !strings.ContainsAny(label, "\\") {
			name = "MusicFS-" + label
		}
	}
	// 通配监听不传入跨网卡的地址集合，zeroconf 按接收接口的 ifIndex 生成 A/AAAA。
	// 明确绑定单个地址时只发布该地址，即使同一网卡还配置了其他地址。
	var addresses []string
	if !listener.IP.IsUnspecified() {
		addresses = []string{listener.IP.String()}
	}
	s, err := zeroconf.RegisterProxy(name, ServiceType, "local", listener.Port, serviceHost, addresses, txt, usable)
	if err != nil {
		return nil, err
	}
	return s.Shutdown, nil
}
