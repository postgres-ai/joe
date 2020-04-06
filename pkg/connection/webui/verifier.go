/*
2019 © Postgres.ai
*/

package webui

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/pkg/log"
)

// Verification constants.
const (
	VerificationSignatureKey = "Verification-Signature"
	signaturePrefix          = "v0="
	bodyPrefix               = "v0:"
)

// Verifier provides a Platform requests verifier.
type Verifier struct {
	bodyPrefix []byte
	secret     []byte
}

// NewVerifier provides a new verifier.
func NewVerifier(secret []byte) *Verifier {
	bodyPrefix := []byte(bodyPrefix)

	return &Verifier{bodyPrefix: bodyPrefix, secret: secret}
}

// Handler provides a middleware to verify incoming requests.
func (a *Verifier) Handler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := a.verifyRequest(r); err != nil {
			log.Dbg("Message filtered due to the signature verification failed:", err.Error())
			w.WriteHeader(http.StatusForbidden)

			return
		}

		h.ServeHTTP(w, r)
	}
}

// verifyRequest verifies a request coming from Platform.
func (a *Verifier) verifyRequest(r *http.Request) error {
	verificationSignature := r.Header.Get(VerificationSignatureKey)
	if verificationSignature == "" {
		return errors.Errorf("%q not found", VerificationSignatureKey)
	}

	signature, err := hex.DecodeString(strings.TrimPrefix(verificationSignature, signaturePrefix))
	if err != nil {
		return errors.Wrap(err, "failed to decode a request signature")
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return errors.Wrap(err, "failed to read the request body")
	}

	// Set a body with the same data we read.
	r.Body = ioutil.NopCloser(bytes.NewBuffer(body))

	if !a.validMAC(body, signature) {
		return errors.Errorf("invalid %q given", VerificationSignatureKey)
	}

	return nil
}

// validMAC reports whether signature is a valid HMAC tag for request body.
func (a *Verifier) validMAC(body, signature []byte) bool {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write(a.bodyPrefix) // nolint: errcheck
	mac.Write(body)         // nolint: errcheck

	expectedMAC := mac.Sum(nil)

	return hmac.Equal(signature, expectedMAC)
}
