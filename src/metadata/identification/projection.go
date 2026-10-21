package identification

// Project uses only non-invalidated evidence. Provider claims remain independent
// from server verification; a measured value never erases the original claim.
func (a *Asset) Project(evidence []Evidence) {
	a.Evidence = []Evidence{}
	a.BitrateBps, a.BitrateOrigin = nil, nil
	a.BitrateMode = "unknown"
	a.ProviderSHA512, a.FileSHA512, a.HashComputedAt = nil, nil, nil
	a.HashStatus = "unknown"
	a.HashConflict = nil
	if !a.Current {
		a.HashStatus = "invalidated"
		return
	}
	priority := map[string]int{"provider": 1, "estimate": 2, "probe": 3, "manual": 4}
	rank := 0
	for _, e := range evidence {
		if e.InvalidatedAt != nil || e.Status == "invalidated" {
			continue
		}
		a.Evidence = append(a.Evidence, e)
		switch e.Kind {
		case "bitrate":
			if e.Status == "reported" && priority[e.Origin] >= rank {
				rank = priority[e.Origin]
				origin := e.Origin
				a.BitrateBps, a.BitrateMode, a.BitrateOrigin = e.BitrateBps, e.BitrateMode, &origin
			}
		case "providerSha512":
			if e.Status == "reported" {
				a.ProviderSHA512 = e.SHA512
			}
		case "fileSha512":
			a.HashStatus = e.Status
			if e.Status == "verified" {
				a.FileSHA512, a.HashComputedAt = e.SHA512, e.ComputedAt
			}
		}
	}
}
