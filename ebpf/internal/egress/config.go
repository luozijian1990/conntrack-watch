package egress

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Ports            []uint16 `yaml:"ports"`
	SourceCIDRs      []string `yaml:"source_cidrs"`
	DestinationCIDRs []string `yaml:"destination_cidrs"`
	ExcludeCIDRs     []string `yaml:"exclude_cidrs"`
	ObjectPath       string   `yaml:"object_path"`
	HostNetNSPath    string   `yaml:"host_netns_path"`
	ListenAddr       string   `yaml:"listen_addr"`
	Node             string   `yaml:"node"`
	LogAttempts      bool     `yaml:"log_attempts"`
	Log              struct {
		Path       string `yaml:"path"`
		MaxSizeMB  int    `yaml:"max_size_mb"`
		MaxBackups int    `yaml:"max_backups"`
		MaxAgeDays int    `yaml:"max_age_days"`
	} `yaml:"log"`
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return ParseConfig(data)
}

func ParseConfig(data []byte) (Config, error) {
	c := Config{Ports: []uint16{80, 443}, ObjectPath: "build/egress.bpf.o", HostNetNSPath: "/proc/1/ns/net", ListenAddr: ":9359"}
	c.Log.MaxSizeMB, c.Log.MaxBackups, c.Log.MaxAgeDays = 100, 10, 7
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	var extra interface{}
	if err := d.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("expected exactly one YAML document")
	}
	if len(c.Ports) == 0 {
		return c, fmt.Errorf("ports must contain at least one target port")
	}
	seen := map[uint16]bool{}
	for _, p := range c.Ports {
		if p == 0 || seen[p] {
			return c, fmt.Errorf("invalid or duplicate port: %d", p)
		}
		seen[p] = true
	}
	if len(c.SourceCIDRs) == 0 {
		return c, fmt.Errorf("source_cidrs must explicitly select Pod/container ranges")
	}
	if _, err := NewFilter(c); err != nil {
		return c, err
	}
	if c.ObjectPath == "" || c.HostNetNSPath == "" {
		return c, fmt.Errorf("object_path and host_netns_path are required")
	}
	if _, _, err := net.SplitHostPort(c.ListenAddr); err != nil {
		return c, fmt.Errorf("listen_addr: %w", err)
	}
	if c.Log.MaxSizeMB <= 0 || c.Log.MaxBackups <= 0 || c.Log.MaxAgeDays <= 0 {
		return c, fmt.Errorf("log rotation limits must be positive")
	}
	if c.Node == "" {
		c.Node = os.Getenv("NODE_NAME")
	}
	if c.Node == "" {
		c.Node, _ = os.Hostname()
	}
	return c, nil
}

type Filter struct {
	sources, destinations, excludes []netip.Prefix
	ports                           map[uint16]bool
}

func NewFilter(c Config) (*Filter, error) {
	f := &Filter{ports: map[uint16]bool{}}
	for _, p := range c.Ports {
		f.ports[p] = true
	}
	for _, item := range []struct {
		name   string
		input  []string
		output *[]netip.Prefix
	}{
		{"source_cidrs", c.SourceCIDRs, &f.sources}, {"destination_cidrs", c.DestinationCIDRs, &f.destinations}, {"exclude_cidrs", c.ExcludeCIDRs, &f.excludes},
	} {
		for _, s := range item.input {
			p, err := netip.ParsePrefix(s)
			if err != nil || p.Addr().Is4In6() {
				return nil, fmt.Errorf("%s: invalid CIDR %q (use native IPv4/IPv6 prefixes)", item.name, s)
			}
			*item.output = append(*item.output, p.Masked())
		}
	}
	return f, nil
}
func contains(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
func (f *Filter) Match(e Event) bool {
	return f.ports[e.DestinationPort] && contains(f.sources, e.SourceIP) &&
		(len(f.destinations) == 0 || contains(f.destinations, e.DestinationIP)) && !contains(f.excludes, e.DestinationIP)
}
