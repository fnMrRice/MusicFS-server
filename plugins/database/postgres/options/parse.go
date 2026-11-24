// Package options 校验 PostgreSQL 部署参数并补齐默认值；解析阶段不访问数据库或凭据。
package options

import (
	"errors"
	"strconv"
	"strings"
)

type Options struct {
	Host, Port, Database, User string
	SSLMode, SSLRootCert       string
	PasswordFile, PasswordEnv  string
}

func Parse(values map[string]string) (Options, error) {
	for key, value := range values {
		switch key {
		case "host", "port", "database", "user", "sslMode":
		case "sslRootCert", "passwordFile", "passwordEnv":
			if value == "" {
				return Options{}, errors.New("empty PostgreSQL storage option")
			}
		default:
			return Options{}, errors.New("unknown PostgreSQL storage option")
		}
		if len(value) > 4096 || strings.ContainsRune(value, 0) {
			return Options{}, errors.New("invalid PostgreSQL storage option")
		}
	}
	o := Options{
		Host: values["host"], Port: values["port"], Database: values["database"], User: values["user"],
		SSLMode: values["sslMode"], SSLRootCert: values["sslRootCert"],
		PasswordFile: values["passwordFile"], PasswordEnv: values["passwordEnv"],
	}
	if strings.TrimSpace(o.Host) == "" || strings.TrimSpace(o.Database) == "" || strings.TrimSpace(o.User) == "" {
		return Options{}, errors.New("PostgreSQL storage requires host, database and user")
	}
	if _, set := values["port"]; !set {
		o.Port = "5432"
	}
	if n, err := strconv.ParseUint(o.Port, 10, 16); err != nil || n == 0 {
		return Options{}, errors.New("invalid PostgreSQL port")
	}
	if _, set := values["sslMode"]; !set {
		o.SSLMode = "verify-full"
	}
	switch o.SSLMode {
	case "disable", "require", "verify-ca", "verify-full":
	default:
		return Options{}, errors.New("invalid PostgreSQL sslMode")
	}
	if o.PasswordFile != "" && o.PasswordEnv != "" {
		return Options{}, errors.New("select one PostgreSQL password source")
	}
	return o, nil
}
