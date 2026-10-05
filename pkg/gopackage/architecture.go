package gopackage

import (
	"fmt"
	"strconv"
	"strings"
)

func architectureTags(arch string) ([]string, error) {
	var tags []string
	switch arch {
	case "amd64":
		value := envOr("GOAMD64", "v1")
		if len(value) != 2 || value[0] != 'v' || value[1] < '1' || value[1] > '4' {
			return nil, fmt.Errorf("invalid GOAMD64 %q", value)
		}
		for level := '1'; level <= rune(value[1]); level++ {
			tags = append(tags, "amd64.v"+string(level))
		}
	case "386":
		value := envOr("GO386", "sse2")
		if value != "sse2" && value != "softfloat" {
			return nil, fmt.Errorf("invalid GO386 %q", value)
		}
		tags = append(tags, "386."+value)
	case "arm":
		value := envOr("GOARM", "7")
		version, suffix, _ := strings.Cut(value, ",")
		if len(version) != 1 || version[0] < '5' || version[0] > '7' ||
			(suffix != "" && suffix != "softfloat" && suffix != "hardfloat") {
			return nil, fmt.Errorf("invalid GOARM %q", value)
		}
		for level := '5'; level <= rune(version[0]); level++ {
			tags = append(tags, "arm."+string(level))
		}
	case "arm64":
		value := envOr("GOARM64", "v8.0")
		parts := strings.Split(value, ",")
		version := parts[0]
		if len(version) != 4 || version[0] != 'v' || version[2] != '.' ||
			(version[1] != '8' && version[1] != '9') || version[3] < '0' ||
			version[3] > '9' || (version[1] == '9' && version[3] > '5') {
			return nil, fmt.Errorf("invalid GOARM64 %q", value)
		}
		for _, suffix := range parts[1:] {
			if suffix != "lse" && suffix != "crypto" {
				return nil, fmt.Errorf("invalid GOARM64 %q", value)
			}
		}
		major, minor := int(version[1]-'0'), int(version[3]-'0')
		for level := 0; level <= minor; level++ {
			tags = append(tags, fmt.Sprintf("arm64.v%d.%d", major, level))
		}
		if major == 9 {
			for level := 0; level <= min(minor+5, 9); level++ {
				tags = append(tags, fmt.Sprintf("arm64.v8.%d", level))
			}
		}
	case "mips", "mipsle", "mips64", "mips64le":
		variable := "GOMIPS"
		if strings.HasPrefix(arch, "mips64") {
			variable = "GOMIPS64"
		}
		value := envOr(variable, "hardfloat")
		if value != "softfloat" && value != "hardfloat" {
			return nil, fmt.Errorf("invalid %s %q", variable, value)
		}
		tags = append(tags, arch+"."+value)
	case "ppc64", "ppc64le":
		value := envOr("GOPPC64", "power8")
		version, err := strconv.Atoi(strings.TrimPrefix(value, "power"))
		if err != nil || !strings.HasPrefix(value, "power") || version < 8 || version > 10 {
			return nil, fmt.Errorf("invalid GOPPC64 %q", value)
		}
		for level := 8; level <= version; level++ {
			tags = append(tags, fmt.Sprintf("%s.power%d", arch, level))
		}
	case "riscv64":
		value := envOr("GORISCV64", "rva20u64")
		if value != "rva20u64" && value != "rva22u64" && value != "rva23u64" {
			return nil, fmt.Errorf("invalid GORISCV64 %q", value)
		}
		for _, level := range []string{"rva20u64", "rva22u64", "rva23u64"} {
			tags = append(tags, "riscv64."+level)
			if level == value {
				break
			}
		}
	case "wasm":
		for suffix := range strings.SplitSeq(envOr("GOWASM", ""), ",") {
			if suffix != "" && suffix != "satconv" && suffix != "signext" {
				return nil, fmt.Errorf("invalid GOWASM feature %q", suffix)
			}
		}
		tags = []string{"wasm.satconv", "wasm.signext"}
	}
	return tags, nil
}
