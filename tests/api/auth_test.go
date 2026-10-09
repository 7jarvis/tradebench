package api_test

import (
	"net/http"
	"testing"

	"github.com/7jarvis/tradebench/tests/internal/check"
	"github.com/7jarvis/tradebench/tests/internal/config"
	"github.com/7jarvis/tradebench/tests/internal/fixtures"
)

func TestAuth_RejectsMissingOrInvalidToken(t *testing.T) {
	t.Parallel()
	valid := config.Get().APIToken
	cases := map[string]string{
		"no token":          "",
		"wrong token":       "definitely-not-the-token",
		"token with suffix": valid + "x",
		"token prefix only": valid[:len(valid)-1],
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			broker := fixtures.BrokerWithToken(t, token)

			_, err := broker.OpenAccount(t.Context(), "should not be created")

			check.APIError(t, err, http.StatusUnauthorized, "unauthorized")
		})
	}
}

func TestAuth_ProbesAreReachableWithoutToken(t *testing.T) {
	t.Parallel()
	broker := fixtures.BrokerWithToken(t, "")

	check.NoError(t, broker.Ready(t.Context()), "readyz without token")
}
