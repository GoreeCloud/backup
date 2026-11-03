package scheduler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const (
	SchemaVersion      = 1
	MaxPolicies        = 4
	MaxConfigBytes     = 64 << 10
	MissedWindow       = 10 * time.Minute
	MaxIntervalMinutes = 7 * 24 * 60
)

var policyID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,47}$`)

// Configuration is a separate, explicitly opted-in private file. Unlike the
// status-only journal it holds source paths and must not be checked into Git.
type Configuration struct {
	Schema   int      `json:"schema"`
	Policies []Policy `json:"policies"`
}

type Policy struct {
	ID              string `json:"id"`
	Enabled         bool   `json:"enabled"`
	Source          string `json:"source"`
	IntervalMinutes int    `json:"interval_minutes"`
	OffsetMinutes   int    `json:"offset_minutes"`
	VerifyAfter     string `json:"verify_after"`
	AcknowledgeFull bool   `json:"acknowledge_full_verification_cost"`
}

// PrivateFile reads a bounded, non-symlink regular file with owner-only access.
// It rechecks the opened file to reject simple path replacement races.
func PrivateFile(name string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(name) {
		return nil, errors.New("private file path must be absolute")
	}
	before, err := os.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("inspect private file: %w", err)
	}
	if !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 ||
		before.Mode().Perm()&0400 == 0 || before.Size() > limit {
		return nil, errors.New("private file must be readable, regular, owner-only and within size limit")
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() ||
		after.Mode().Perm()&0077 != 0 || after.Size() > limit {
		return nil, errors.New("private file changed during opening or is not private")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("private file could not be read within its size limit")
	}
	return data, nil
}

func decodeOne(data []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("invalid private JSON: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("private JSON must contain exactly one object")
	}
	return nil
}

func LoadConfiguration(filename string) (Configuration, error) {
	data, err := PrivateFile(filename, MaxConfigBytes)
	if err != nil {
		return Configuration{}, err
	}
	var cfg Configuration
	if err := decodeOne(data, &cfg); err != nil {
		return Configuration{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Configuration{}, err
	}
	return cfg, nil
}

func (c Configuration) Validate() error {
	if c.Schema != SchemaVersion || len(c.Policies) == 0 || len(c.Policies) > MaxPolicies {
		return errors.New("unsupported schedule schema or policy count (1 to 4)")
	}
	seen := map[string]bool{}
	for _, p := range c.Policies {
		if !policyID.MatchString(p.ID) || seen[p.ID] {
			return errors.New("policy ID is invalid or duplicated")
		}
		seen[p.ID] = true
		if !filepath.IsAbs(p.Source) || filepath.Clean(p.Source) == string(filepath.Separator) ||
			len(p.Source) > 2048 || filepath.Clean(p.Source) != p.Source ||
			strings.IndexFunc(p.Source, unicode.IsControl) >= 0 {
			return fmt.Errorf("policy %s has invalid absolute non-root source", p.ID)
		}
		if p.IntervalMinutes < 60 || p.IntervalMinutes > MaxIntervalMinutes ||
			p.OffsetMinutes < 0 || p.OffsetMinutes >= p.IntervalMinutes {
			return fmt.Errorf("policy %s has unsupported interval or UTC offset", p.ID)
		}
		switch p.VerifyAfter {
		case "none", "structure":
			if p.AcknowledgeFull {
				return fmt.Errorf("policy %s has extraneous full verification acknowledgment", p.ID)
			}
		case "full":
			if !p.AcknowledgeFull {
				return fmt.Errorf("policy %s requires full verification cost acknowledgment", p.ID)
			}
		default:
			return fmt.Errorf("policy %s must select none, structure, or full verification", p.ID)
		}
	}
	return nil
}

// SlotStart uses UTC epoch-aligned intervals to avoid DST ambiguities.
// A policy is considered due only during the first ten minutes of its slot.
func SlotStart(p Policy, now time.Time) (time.Time, bool) {
	secs := int64(p.IntervalMinutes) * 60
	offset := int64(p.OffsetMinutes) * 60
	n := now.UTC().Unix()
	// All real scheduling timestamps are after the Unix epoch.
	if n < offset {
		return time.Time{}, false
	}
	slot := ((n-offset)/secs)*secs + offset
	start := time.Unix(slot, 0).UTC()
	return start, n-slot < int64(MissedWindow/time.Second)
}
