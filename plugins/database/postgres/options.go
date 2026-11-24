package postgres

import (
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/postgres/options"
	"github.com/jackc/pgx/v5"
)

// connectionConfig 从部署配置构造连接参数；凭据只在内存中交给驱动。
func connectionConfig(values map[string]string) (*pgx.ConnConfig, error) {
	o, err := options.Parse(values)
	if err != nil {
		return nil, err
	}
	query := url.Values{
		"host": {o.Host}, "port": {o.Port}, "dbname": {o.Database}, "user": {o.User},
		"sslmode": {o.SSLMode}, "application_name": {"MusicFS"}, "search_path": {"public"},
	}
	if o.SSLRootCert != "" {
		query.Set("sslrootcert", o.SSLRootCert)
	}
	// PostgreSQL URI 用 %20 表示空格，保留证书路径、账号和库名中的空格及加号。
	config, err := pgx.ParseConfig("postgres:///?" + strings.ReplaceAll(query.Encode(), "+", "%20"))
	if err != nil {
		return nil, errors.New("invalid PostgreSQL connection or TLS configuration")
	}
	config.ConnectTimeout = 10 * time.Second
	switch {
	case o.PasswordFile != "":
		password, err := readPassword(o.PasswordFile)
		if err != nil {
			return nil, err
		}
		config.Password = password
	case o.PasswordEnv != "":
		password, ok := os.LookupEnv(o.PasswordEnv)
		if !ok || password == "" {
			return nil, errors.New("PostgreSQL password environment variable is empty")
		}
		config.Password = password
	}
	return config, nil
}

func readPassword(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", errors.New("cannot read PostgreSQL password file")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	password := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if err != nil || len(data) > 4096 || password == "" || strings.ContainsAny(password, "\r\n\x00") {
		return "", errors.New("invalid PostgreSQL password file")
	}
	return password, nil
}
