package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIPv6PortMappingAccessPreservesTenantBoundaries(t *testing.T) {
	token := loginFirewallSubUser(t)
	handler := AuthMiddleware(SubUserMiddleware(HandleSingleContainer))
	for _, prefix := range []string{"/api/containers/", "/api/v1/containers/"} {
		response := requestFirewallAs(handler, token, http.MethodGet, prefix+"11/ipv6-port-mappings", "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"sync_ipv4":true`) {
			t.Fatalf("owned IPv6 inventory = %d: %s", response.Code, response.Body.String())
		}
		for _, request := range []struct{ method, action string }{
			{http.MethodPost, "11/ipv6-port-mappings"},
			{http.MethodDelete, "11/ipv6-port-mappings/0"},
			{http.MethodPut, "11/ipv6-nat-sync"},
			{http.MethodGet, "12/ipv6-port-mappings"},
			{http.MethodPut, "12/ipv6-port-mappings/0"},
		} {
			response := requestFirewallAs(handler, token, request.method, prefix+request.action, `{"enabled":false}`)
			if response.Code != http.StatusForbidden {
				t.Fatalf("tenant %s %s = %d, want forbidden: %s", request.method, request.action, response.Code, response.Body.String())
			}
		}
	}
}

func TestIPv6PortMappingRequiresNetworkScope(t *testing.T) {
	loginFirewallSubUser(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/containers/11/ipv6-port-mappings", nil)
	request = withAuthContext(request, AuthContext{Type: authTypeAPIKey, Scopes: []string{"container:read"}, ContainerUUIDs: []string{"owned-uuid"}})
	response := httptest.NewRecorder()
	HandleSingleContainer(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("IPv6 inventory allowed read-only token: %d", response.Code)
	}
}
