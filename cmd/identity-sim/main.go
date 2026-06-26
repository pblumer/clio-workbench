package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	url         = flag.String("url", "https://clio.blumer.cloud", "Clio base URL")
	token       = flag.String("token", "", "Clio API token (Bearer)")
	interval    = flag.Duration("interval", 30*time.Second, "Interval between identities")
	count       = flag.Int("count", -1, "Total identities to create (-1 = infinite)")
	startID     = flag.Int("start-id", 10, "Starting employee ID number")
	internalPct = flag.Int("internal-pct", 70, "Percentage of internal employees (0-100)")
)

func main() {
	flag.Parse()
	if *token == "" {
		if t := os.Getenv("CLIO_API_TOKEN"); t != "" {
			*token = t
		} else {
			log.Fatal("--token or CLIO_API_TOKEN required")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	seq := *startID
	created := 0

	log.Printf("identity-sim starting: url=%s interval=%s count=%d", *url, *interval, *count)

	t := time.NewTicker(*interval)
	defer t.Stop()

	for {
		select {
		case <-sig:
			log.Println("shutting down")
			return
		case <-ctx.Done():
			return
		case <-t.C:
			if err := runIdentity(ctx, *url, *token, seq); err != nil {
				log.Printf("identity %d failed: %v", seq, err)
			} else {
				created++
				log.Printf("identity %d created (%d total)", seq, created)
			}
			seq++
			if *count > 0 && created >= *count {
				log.Println("count reached, shutting down")
				return
			}
		}
	}
}

func runIdentity(ctx context.Context, baseURL, token string, n int) error {
	isInternal := rand.Intn(100) < *internalPct
	empType := "external"
	if isInternal {
		empType = "internal"
	}

	eid := fmt.Sprintf("E-000%03d", n)
	pua := fmt.Sprintf("PUA-000%03d", n)
	sua := fmt.Sprintf("SUA-000%03d", n)
	adm := fmt.Sprintf("ADM-000%03d", n)
	tst := fmt.Sprintf("TST-000%03d", n)

	if err := appendEvent(ctx, baseURL, token, "identity", "/employees/"+eid, "employee.created",
		map[string]any{
			"firstName":  fmt.Sprintf("User%d", n),
			"lastName":   "Simulated",
			"department": randomDept(),
			"type":       empType,
		}); err != nil {
		return fmt.Errorf("employee created: %w", err)
	}

	if err := appendEvent(ctx, baseURL, token, "identity", "/primary-accounts/"+pua, "primary-account.created",
		map[string]any{
			"employeeId": eid,
			"username":   fmt.Sprintf("user%d", n),
			"email":      fmt.Sprintf("user%d@example.com", n),
			"status":     "active",
		}); err != nil {
		return fmt.Errorf("primary created: %w", err)
	}

	if err := appendEvent(ctx, baseURL, token, "identity", "/employees/"+eid, "primary-account.assigned",
		map[string]any{
			"primaryAccountId": pua,
			"linkedAt":         time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
		return fmt.Errorf("primary assigned: %w", err)
	}
	if err := appendEvent(ctx, baseURL, token, "identity", "/primary-accounts/"+pua, "employee.assigned",
		map[string]any{
			"employeeId": eid,
		}); err != nil {
		return fmt.Errorf("employee assigned: %w", err)
	}

	if err := appendEvent(ctx, baseURL, token, "identity", "/secondary-accounts/"+sua, "secondary-account.created",
		map[string]any{
			"employeeId":            eid,
			"ownerPrimaryAccountId": pua,
			"username":              fmt.Sprintf("user%d-sec", n),
			"purpose":               randomPurpose(),
			"status":                "active",
		}); err != nil {
		return fmt.Errorf("secondary created: %w", err)
	}

	if err := appendEvent(ctx, baseURL, token, "identity", "/primary-accounts/"+pua, "secondary-account.linked",
		map[string]any{
			"secondaryAccountId": sua,
			"linkedAt":         time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
		return fmt.Errorf("secondary linked: %w", err)
	}
	if err := appendEvent(ctx, baseURL, token, "identity", "/secondary-accounts/"+sua, "owner.assigned",
		map[string]any{
			"ownerPrimaryAccountId": pua,
		}); err != nil {
		return fmt.Errorf("owner assigned: %w", err)
	}

	if isInternal {
		if err := appendEvent(ctx, baseURL, token, "identity", "/admin-accounts/"+adm, "admin-account.created",
			map[string]any{
				"employeeId":  eid,
				"level":       randomLevel(),
				"permissions": []string{"user.read", "user.write"},
			}); err != nil {
			return fmt.Errorf("admin created: %w", err)
		}
		if err := appendEvent(ctx, baseURL, token, "identity", "/employees/"+eid, "admin-account.assigned",
			map[string]any{
				"adminAccountId": adm,
				"grantedAt":      time.Now().UTC().Format(time.RFC3339),
			}); err != nil {
			return fmt.Errorf("admin assigned: %w", err)
		}
		if err := appendEvent(ctx, baseURL, token, "identity", "/admin-accounts/"+adm, "employee.assigned",
			map[string]any{
				"employeeId": eid,
			}); err != nil {
			return fmt.Errorf("admin employee assigned: %w", err)
		}
	}

	if rand.Intn(100) < 55 {
		if err := appendEvent(ctx, baseURL, token, "identity", "/test-accounts/"+tst, "test-account.created",
			map[string]any{
				"createdBy":  eid,
				"scope":      randomScope(),
				"expiryDate": time.Now().AddDate(0, 3, 0).Format("2006-01-02"),
			}); err != nil {
			return fmt.Errorf("test created: %w", err)
		}
		if err := appendEvent(ctx, baseURL, token, "identity", "/employees/"+eid, "test-account.assigned",
			map[string]any{
				"testAccountId": tst,
				"linkedAt":      time.Now().UTC().Format(time.RFC3339),
			}); err != nil {
			return fmt.Errorf("test assigned: %w", err)
		}
		if err := appendEvent(ctx, baseURL, token, "identity", "/test-accounts/"+tst, "employee.assigned",
			map[string]any{
				"employeeId": eid,
			}); err != nil {
			return fmt.Errorf("test employee assigned: %w", err)
		}
	}

	return nil
}

func appendEvent(ctx context.Context, baseURL, token, source, subject, typ string, data map[string]any) error {
	batch := map[string]any{
		"events": []any{
			map[string]any{
				"source":  source,
				"subject": subject,
				"type":    typ,
				"data":    data,
			},
		},
	}
	body, _ := json.Marshal(batch)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/write-events", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("clio: append returned HTTP %d", resp.StatusCode)
}

func randomDept() string {
	depts := []string{"Engineering", "HR", "Sales", "Finance", "Operations", "Legal", "Marketing"}
	return depts[rand.Intn(len(depts))]
}

func randomPurpose() string {
	p := []string{"shared-inbox", "support", "automation", "backup"}
	return p[rand.Intn(len(p))]
}

func randomLevel() string {
	l := []string{"super", "regional", "support"}
	return l[rand.Intn(len(l))]
}

func randomScope() string {
	s := []string{"integration", "e2e", "performance", "manual"}
	return s[rand.Intn(len(s))]
}
