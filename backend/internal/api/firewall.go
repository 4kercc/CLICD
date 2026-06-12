package api

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"strconv"
	"strings"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

func generateFirewallRuleID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func getFirewall(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"enabled": c.FirewallEnabled,
			"rules":   c.FirewallRules,
		},
	})
}

func updateFirewall(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}

	var req struct {
		Enabled *bool               `json:"enabled"`
		Rules   *[]config.FirewallRule `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	if req.Enabled != nil {
		c.FirewallEnabled = *req.Enabled
	}
	if req.Rules != nil {
		// Validate and assign IDs to new rules
		rules := *req.Rules
		for i := range rules {
			rules[i].Direction = strings.ToLower(strings.TrimSpace(rules[i].Direction))
			rules[i].Protocol = strings.ToLower(strings.TrimSpace(rules[i].Protocol))
			rules[i].Action = strings.ToUpper(strings.TrimSpace(rules[i].Action))
			rules[i].SourceIP = strings.TrimSpace(rules[i].SourceIP)
			rules[i].Port = strings.TrimSpace(rules[i].Port)

			if rules[i].Direction != "in" && rules[i].Direction != "out" {
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid direction: " + rules[i].Direction})
				return
			}
			if rules[i].Protocol != "tcp" && rules[i].Protocol != "udp" && rules[i].Protocol != "icmp" && rules[i].Protocol != "all" {
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid protocol: " + rules[i].Protocol})
				return
			}
			if rules[i].Action != "ACCEPT" && rules[i].Action != "DROP" {
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid action: " + rules[i].Action})
				return
			}
			if rules[i].ID == "" {
				rules[i].ID = generateFirewallRuleID()
			}
			// Validate port spec
			if rules[i].Port != "" {
				if err := validatePortSpec(rules[i].Port); err != nil {
					jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid port: " + err.Error()})
					return
				}
			}
		}
		c.FirewallRules = rules
	}

	config.SaveConfig()

	// Apply firewall rules to iptables if container is running
	if c.Status == "running" {
		if err := lxc.ApplyFirewallRules(id); err != nil {
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to apply firewall rules: " + err.Error()})
			return
		}
	} else if !c.FirewallEnabled {
		// If disabled and not running, clean any lingering rules
		lxc.CleanFirewallRules(id)
	}

	jsonResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Firewall updated",
		Data: map[string]interface{}{
			"enabled": c.FirewallEnabled,
			"rules":   c.FirewallRules,
		},
	})
}

func validatePortSpec(port string) error {
	port = strings.TrimSpace(port)
	if port == "" {
		return nil
	}
	// Support: "22", "80,443", "8000-9000", "80,443,8000-9000"
	for _, part := range strings.Split(port, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			// Range
			bounds := strings.SplitN(part, "-", 2)
			lo, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil || lo < 1 || lo > 65535 {
				return &portValidationError{part}
			}
			hi, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil || hi < 1 || hi > 65535 {
				return &portValidationError{part}
			}
		} else {
			p, err := strconv.Atoi(part)
			if err != nil || p < 1 || p > 65535 {
				return &portValidationError{part}
			}
		}
	}
	return nil
}

type portValidationError struct {
	port string
}

func (e *portValidationError) Error() string {
	return "invalid port value: " + e.port
}
