package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"clicd/internal/config"
)

func HandleIPv6Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "ipv6:read") {
		return
	}
	status := lxcManager.DetectIPv6Status()
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: status})
}

func assignIPv6(w http.ResponseWriter, r *http.Request, id int) {
	c, err := assignIPv6ByRuntime(id)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "IPv6 assigned", Data: c})
}

type ipAssignmentRequest struct {
	Mode      string   `json:"mode"`
	Auto      *bool    `json:"auto,omitempty"`
	Count     int      `json:"count,omitempty"`
	Addresses []string `json:"addresses,omitempty"`
}

func (req ipAssignmentRequest) allocation() ([]string, int, bool) {
	auto := req.Mode == "random" || req.Mode == "auto"
	if req.Mode == "custom" {
		auto = false
	}
	if req.Mode == "clear" || req.Mode == "none" {
		return nil, 0, false
	}
	if req.Auto != nil {
		auto = *req.Auto
	}
	return req.Addresses, req.Count, auto
}

func updatePublicIPv4(w http.ResponseWriter, r *http.Request, id int) {
	var req ipAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	addresses, count, auto := req.allocation()
	target := containerAuditTarget(id)
	before := assignedPublicIPv4Addresses(id)
	c, err := updatePublicIPv4ByRuntime(id, addresses, count, auto)
	if err != nil {
		config.AddAuditLogFull("container.public_ipv4", target, err.Error(), requestActor(r), clientIP(r), r.UserAgent(), false, err.Error())
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	config.AddAuditLogFull("container.public_ipv4", c.Name,
		describeAddressChange(before, assignedPublicIPv4Addresses(id)),
		requestActor(r), clientIP(r), r.UserAgent(), true, "")
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Public IPv4 assignments updated", Data: c})
}

func updateIPv6Addresses(w http.ResponseWriter, r *http.Request, id int) {
	var req ipAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	addresses, count, auto := req.allocation()
	target := containerAuditTarget(id)
	before := assignedIPv6Addresses(id)
	c, err := updateIPv6ByRuntime(id, addresses, count, auto)
	if err != nil {
		config.AddAuditLogFull("container.public_ipv6", target, err.Error(), requestActor(r), clientIP(r), r.UserAgent(), false, err.Error())
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	config.AddAuditLogFull("container.public_ipv6", c.Name,
		describeAddressChange(before, assignedIPv6Addresses(id)),
		requestActor(r), clientIP(r), r.UserAgent(), true, "")
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "IPv6 assignments updated", Data: c})
}

// containerAuditTarget names a container for the audit trail, falling back to
// its id when it is already gone.
func containerAuditTarget(id int) string {
	if c := config.FindContainer(id); c != nil && strings.TrimSpace(c.Name) != "" {
		return c.Name
	}
	return fmt.Sprintf("container %d", id)
}

// describeAddressChange renders an assignment change for the audit trail, for
// example "23.95.253.11 -> 23.95.253.10". Public addresses get moved between
// instances often enough that the trail has to say which address moved, not
// just that something changed.
func describeAddressChange(before, after []string) string {
	from := "(none)"
	if len(before) > 0 {
		from = strings.Join(before, ",")
	}
	to := "(none)"
	if len(after) > 0 {
		to = strings.Join(after, ",")
	}
	return from + " -> " + to
}

func assignedPublicIPv4Addresses(id int) []string {
	c := config.FindContainer(id)
	if c == nil {
		return nil
	}
	addresses := make([]string, 0, len(c.PublicIPv4s))
	for _, item := range c.PublicIPv4s {
		if address := strings.TrimSpace(item.Address); address != "" {
			addresses = append(addresses, address)
		}
	}
	return addresses
}

func assignedIPv6Addresses(id int) []string {
	c := config.FindContainer(id)
	if c == nil {
		return nil
	}
	addresses := make([]string, 0, len(c.IPv6Addresses))
	for _, item := range c.IPv6Addresses {
		if address := strings.TrimSpace(item.Address); address != "" {
			addresses = append(addresses, address)
		}
	}
	return addresses
}
