package catalog

// Category 是曲目标签或目录的浏览分类，不代替来源歌手、专辑实体身份。
type Category struct {
	ID                 string        `json:"categoryId"`
	Kind               string        `json:"kind"`
	Name               string        `json:"name"`
	Unknown            bool          `json:"unknown"`
	ProviderInstanceID string        `json:"providerInstanceId"`
	RootID             string        `json:"rootId"`
	ParentID           *string       `json:"parentId"`
	TrackCount         int           `json:"trackCount"`
	Breadcrumbs        []CategoryRef `json:"breadcrumbs"`
}

type CategoryRef struct {
	ID   string `json:"categoryId"`
	Name string `json:"name"`
}
