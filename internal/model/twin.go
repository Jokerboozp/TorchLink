package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
)

var ErrTwinConflict = errors.New("twin topology version conflict")

type TwinRelation struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}
type TwinTopology struct {
	TenantID  string         `json:"tenantId"`
	Version   int64          `json:"version"`
	UpdatedAt int64          `json:"updatedAt"`
	Relations []TwinRelation `json:"relations"`
}
type TwinUpdate struct {
	TenantID        string
	ExpectedVersion int64
	Timestamp       int64
	Add             []TwinRelation
	Remove          []string
}

func (r TwinRelation) Key() string {
	sum := sha256.Sum256([]byte(r.Source + "\x00" + r.Kind + "\x00" + r.Target))
	return hex.EncodeToString(sum[:16])
}
func ApplyTwin(old TwinTopology, u TwinUpdate) (TwinTopology, error) {
	if old.Version != u.ExpectedVersion {
		return old, ErrTwinConflict
	}
	if u.TenantID == "" || (old.TenantID != "" && old.TenantID != u.TenantID) || u.Timestamp <= 0 || len(u.Add)+len(u.Remove) == 0 || len(u.Add)+len(u.Remove) > 200 {
		return old, errors.New("invalid topology update")
	}
	relations := map[string]TwinRelation{}
	for _, r := range old.Relations {
		relations[r.ID] = r
	}
	for _, id := range u.Remove {
		delete(relations, id)
	}
	for _, r := range u.Add {
		if r.Source == "" || r.Target == "" || r.Source == r.Target || len(r.Source) > 128 || len(r.Target) > 128 || (r.Kind != "contains" && r.Kind != "monitors" && r.Kind != "depends_on") {
			return old, errors.New("invalid twin relation")
		}
		r.ID = r.Key()
		relations[r.ID] = r
	}
	if len(relations) > 10000 {
		return old, errors.New("topology exceeds 10000 relations")
	}
	// Containment has one parent; containment and dependency cycles are rejected
	// independently. Monitoring is a directed relation without execution semantics.
	for _, kind := range []string{"contains", "depends_on"} {
		adjacency := map[string][]string{}
		parents := map[string]int{}
		nodes := map[string]bool{}
		for _, r := range relations {
			if r.Kind != kind {
				continue
			}
			adjacency[r.Source] = append(adjacency[r.Source], r.Target)
			parents[r.Target]++
			nodes[r.Source] = true
			nodes[r.Target] = true
			if kind == "contains" && parents[r.Target] > 1 {
				return old, errors.New("contained device already has a parent")
			}
		}
		ready := []string{}
		for n := range nodes {
			if parents[n] == 0 {
				ready = append(ready, n)
			}
		}
		visited := 0
		for len(ready) > 0 {
			n := ready[0]
			ready = ready[1:]
			visited++
			for _, target := range adjacency[n] {
				parents[target]--
				if parents[target] == 0 {
					ready = append(ready, target)
				}
			}
		}
		if visited != len(nodes) {
			return old, errors.New("twin relationship would create a cycle")
		}
	}
	next := TwinTopology{TenantID: u.TenantID, Version: old.Version, UpdatedAt: old.UpdatedAt, Relations: []TwinRelation{}}
	changed := len(relations) != len(old.Relations)
	for _, r := range old.Relations {
		if _, ok := relations[r.ID]; !ok {
			changed = true
		}
	}
	for _, r := range relations {
		next.Relations = append(next.Relations, r)
	}
	sort.Slice(next.Relations, func(i, j int) bool { return next.Relations[i].ID < next.Relations[j].ID })
	if changed {
		next.Version++
		next.UpdatedAt = u.Timestamp
	}
	return next, nil
}

type TwinNode struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ProductID        string `json:"productId"`
	Status           string `json:"status"`
	ConnectionStatus string `json:"connectionStatus"`
	BusinessStatus   string `json:"businessStatus"`
	LastSeenAt       int64  `json:"lastSeenAt"`
}
