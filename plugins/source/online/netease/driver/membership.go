package driver

import (
	"context"
	"encoding/json"
	"time"
)

type membershipFields struct {
	VIPType json.RawMessage `json:"vipType"`
	Rights  json.RawMessage `json:"vipRights"`
}

func (p membershipFields) tier() string {
	switch string(p.VIPType) {
	case "0":
		return "free"
	case "11":
		// 两档会员均可返回 11。只有账号资料明确报告已生效的超级会员权益才细分。
		var rights struct {
			Redplus struct {
				Active bool `json:"rights"`
				Code   int  `json:"vipCode"`
			} `json:"redplus"`
		}
		if json.Unmarshal(p.Rights, &rights) == nil && rights.Redplus.Active && rights.Redplus.Code == 300 {
			return "svip"
		}
		return "vip"
	}
	return ""
}

func (c *Client) membershipTier(ctx context.Context, id string, profile membershipFields) string {
	if tier := profile.tier(); tier != "" {
		return tier
	}
	// 登录接口缺少可识别的标志时，在本次校验的时间预算内读取同一账号的官方公开资料。
	// 为主请求预留完成时间，资料查询失败时仍保留已验证的账号身份。
	budget := 3 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		budget = min(budget, time.Until(deadline)-500*time.Millisecond)
	}
	if budget <= 0 {
		return ""
	}
	read, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	if c.WaitForMetadata(read) != nil || c.Admit(read) != nil {
		return ""
	}
	raw, _, err := c.HTTP(read, "GET", "https://interface.music.163.com/api/v1/user/detail/"+id, "", nil, nil)
	var details struct {
		Code    int `json:"code"`
		Profile struct {
			ID json.Number `json:"userId"`
			membershipFields
		} `json:"profile"`
	}
	if err != nil || json.Unmarshal(raw, &details) != nil || details.Code != 200 || details.Profile.ID.String() != id {
		return ""
	}
	return details.Profile.tier()
}
