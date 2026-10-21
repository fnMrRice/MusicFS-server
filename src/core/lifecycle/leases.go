package lifecycle

import "github.com/fnMrRice/MusicFS-server/src/core/domain"

func (m *Manager) Acquire(p Principal, r AcquireRequest, key string) (Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.nowLocked()
	m.expireLocked(now)
	if err := m.principalLocked(p, now); err != nil {
		return Lease{}, err
	}
	if err := m.instanceLocked(r.InstanceID); err != nil {
		return Lease{}, err
	}
	if !identifier.MatchString(r.ClientID) || !identifier.MatchString(key) {
		return Lease{}, domain.Fail("INVALID_ARGUMENT", "Invalid lifecycle client or operation key")
	}
	if err := m.acceptsLocked(); err != nil {
		return Lease{}, err
	}
	opKey := p.SessionID + "/" + key
	if op, ok := m.operations[opKey]; ok {
		if op.request != r {
			return Lease{}, domain.Fail("CONFLICT", "Lifecycle operation key was reused")
		}
		return m.records[op.leaseID].lease, nil
	}
	if len(m.operations) >= MaxRecords || len(m.records) >= MaxRecords || len(m.revoked) >= MaxRecords {
		return Lease{}, domain.Fail("RATE_LIMITED", "Lifecycle operation limit reached")
	}
	for _, old := range m.records {
		if old.principal.SessionID == p.SessionID && old.lease.ClientID == r.ClientID && old.lease.State == "active" {
			m.operations[opKey] = operation{r, old.lease.ID}
			return old.lease, nil
		}
	}
	if m.activeLocked() >= MaxLeases {
		return Lease{}, domain.Fail("RATE_LIMITED", "Lifecycle lease limit reached")
	}
	deadline := leaseExpiry(now, p, m.options.LeaseTTL)
	lease := Lease{ID: domain.NewID(), InstanceID: r.InstanceID, ClientID: r.ClientID, State: "active", CreatedAt: now.UTC(), ExpiresAt: deadline.UTC()}
	m.records[lease.ID] = &record{lease: lease, principal: p, deadline: deadline}
	m.operations[opKey] = operation{r, lease.ID}
	m.everAcquired = true
	return lease, nil
}
func (m *Manager) ownedLocked(p Principal, id string) (*record, error) {
	r, ok := m.records[id]
	if !ok || r.principal.UserID != p.UserID || r.principal.SessionID != p.SessionID {
		return nil, domain.Fail("NOT_FOUND", "Lifecycle lease not found")
	}
	return r, nil
}
func (m *Manager) Renew(p Principal, id string, r RenewRequest) (Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.nowLocked()
	m.expireLocked(now)
	if err := m.principalLocked(p, now); err != nil {
		return Lease{}, err
	}
	if err := m.instanceLocked(r.InstanceID); err != nil {
		return Lease{}, err
	}
	lease, err := m.ownedLocked(p, id)
	if err != nil {
		return Lease{}, err
	}
	if lease.lease.State != "active" {
		return Lease{}, domain.Fail("SESSION_EXPIRED", "Lifecycle lease has ended; discover the server again")
	}
	if err = m.acceptsLocked(); err != nil {
		return Lease{}, err
	}
	if r.Sequence < 1 || r.Sequence > 1<<53-1 {
		return Lease{}, domain.Fail("INVALID_ARGUMENT", "Invalid lifecycle renewal sequence")
	}
	if r.Sequence == lease.lease.Sequence {
		return lease.lease, nil
	}
	if r.Sequence != lease.lease.Sequence+1 {
		return Lease{}, domain.Fail("CONFLICT", "Lifecycle renewal sequence changed")
	}
	lease.lease.Sequence = r.Sequence
	if lease.principal.ExpiresAt.Before(p.ExpiresAt) {
		p.ExpiresAt = lease.principal.ExpiresAt
	}
	lease.deadline = leaseExpiry(now, p, m.options.LeaseTTL)
	lease.lease.ExpiresAt = lease.deadline.UTC()
	return lease.lease, nil
}
func (m *Manager) Release(p Principal, id string, r ReleaseRequest) (Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.nowLocked()
	m.expireLocked(now)
	if err := m.principalLocked(p, now); err != nil {
		return Lease{}, err
	}
	if err := m.instanceLocked(r.InstanceID); err != nil {
		return Lease{}, err
	}
	lease, err := m.ownedLocked(p, id)
	if err != nil {
		return Lease{}, err
	}
	if lease.lease.State == "active" {
		lease.lease.State = "released"
	}
	m.expireLocked(now)
	return lease.lease, nil
}

// RevokeSession also fences requests authenticated immediately before logout.
func (m *Manager) RevokeSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.options.Mode != ClientManaged {
		return
	}
	if len(m.revoked) < MaxRecords {
		m.revoked[sessionID] = true
	}
	for _, r := range m.records {
		if r.principal.SessionID == sessionID && r.lease.State == "active" {
			r.lease.State = "revoked"
		}
	}
	m.expireLocked(m.nowLocked())
}
