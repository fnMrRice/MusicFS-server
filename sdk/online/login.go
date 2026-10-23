package online

import "regexp"

// LoginDescriptor 来自插件，空值表示没有交互式登录。
type LoginDescriptor struct {
	Method     string `json:"method"`
	Validation string `json:"validation"`
}

// AccountOptions 保持 HTTP 登录字段；开发者令牌是插件明确公开给登录客户端的数据。
type AccountOptions struct {
	Method         string `json:"method"`
	DeveloperToken string `json:"developerToken,omitempty"`
	Configured     bool   `json:"configured"`
	Validation     string `json:"validation"`
}

func (d *LoginDescriptor) Validate() error {
	if d == nil {
		return nil
	}
	if !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9.-]{0,63}$`).MatchString(d.Method) || !validText(d.Validation) {
		return invalidResult()
	}
	return nil
}

func (o AccountOptions) Validate(d *LoginDescriptor) error {
	if d == nil {
		if o.Method != "none" || o.Configured || o.DeveloperToken != "" || o.Validation != "notSupported" {
			return invalidResult()
		}
		return nil
	}
	if o.Method != d.Method || o.Validation != d.Validation || len(o.DeveloperToken) > 16384 {
		return invalidResult()
	}
	return nil
}
