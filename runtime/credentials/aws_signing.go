package credentials

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// BedrockEndpoint returns the Bedrock endpoint URL for a region.
func BedrockEndpoint(region string) string {
	return fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", region)
}

// Apply signs the request using AWS SigV4.
func (c *AWSCredential) Apply(ctx context.Context, req *http.Request) error {
	creds, err := c.cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return fmt.Errorf("failed to retrieve AWS credentials: %w", err)
	}

	// Sign the request using SigV4
	return signRequest(req, &creds, c.region, "bedrock")
}

// signRequest signs an HTTP request using AWS SigV4.
func signRequest(req *http.Request, creds *aws.Credentials, region, service string) error {
	t := time.Now().UTC()
	amzDate := t.Format("20060102T150405Z")
	dateStamp := t.Format("20060102")

	// Read and hash the body
	var bodyHash string
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return fmt.Errorf("failed to read request body: %w", err)
		}
		req.Body = io.NopCloser(strings.NewReader(string(body)))
		bodyHash = hashSHA256(body)
	} else {
		bodyHash = hashSHA256([]byte{})
	}

	// Set required headers
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", bodyHash)
	if creds.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", creds.SessionToken)
	}

	// Create canonical request.
	// URI-encode each path segment per SigV4 spec — characters like ':' in
	// Bedrock model IDs (e.g. "v1:0") must be percent-encoded.
	canonicalURI := uriEncodePath(req.URL.Path)
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQueryString := req.URL.RawQuery

	// Get signed headers
	signedHeaders := getSignedHeaders(req)
	canonicalHeaders := getCanonicalHeaders(req, signedHeaders)

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		strings.Join(signedHeaders, ";"),
		bodyHash,
	}, "\n")

	// Create string to sign
	algorithm := "AWS4-HMAC-SHA256"
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	stringToSign := strings.Join([]string{
		algorithm,
		amzDate,
		credentialScope,
		hashSHA256([]byte(canonicalRequest)),
	}, "\n")

	// Calculate signature
	signingKey := getSignatureKey(creds.SecretAccessKey, dateStamp, region, service)
	signature := hmacSHA256Hex(signingKey, stringToSign)

	// Set Authorization header
	authHeader := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm,
		creds.AccessKeyID,
		credentialScope,
		strings.Join(signedHeaders, ";"),
		signature,
	)
	req.Header.Set("Authorization", authHeader)

	return nil
}

// uriEncodePath URI-encodes each segment of a path per the SigV4 spec.
// Slashes are preserved; all other reserved characters are percent-encoded.
func uriEncodePath(path string) string {
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		segments[i] = uriEncode(seg)
	}
	return strings.Join(segments, "/")
}

// uriEncode percent-encodes a URI component per RFC 3986.
// Unreserved characters (A-Z a-z 0-9 - _ . ~) are not encoded.
func uriEncode(s string) string {
	var buf strings.Builder
	for _, b := range []byte(s) {
		if isUnreserved(b) {
			buf.WriteByte(b)
		} else {
			fmt.Fprintf(&buf, "%%%02X", b)
		}
	}
	return buf.String()
}

// isUnreserved returns true for RFC 3986 unreserved characters.
func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~'
}

// getSignedHeaders returns the list of headers to sign, sorted.
func getSignedHeaders(req *http.Request) []string {
	headers := make([]string, 0)
	for name := range req.Header {
		lowerName := strings.ToLower(name)
		// Include all headers except some that shouldn't be signed
		if lowerName != "authorization" && lowerName != "user-agent" {
			headers = append(headers, lowerName)
		}
	}
	// Always include host
	headers = append(headers, "host")
	sort.Strings(headers)
	return headers
}

// getCanonicalHeaders returns the canonical header string.
func getCanonicalHeaders(req *http.Request, signedHeaders []string) string {
	var builder strings.Builder
	for _, name := range signedHeaders {
		if name == "host" {
			fmt.Fprintf(&builder, "host:%s\n", req.Host)
		} else {
			values := req.Header.Values(http.CanonicalHeaderKey(name))
			fmt.Fprintf(&builder, "%s:%s\n", name, strings.Join(values, ","))
		}
	}
	return builder.String()
}

// hashSHA256 returns the SHA256 hash of data as a hex string.
func hashSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// hmacSHA256 returns HMAC-SHA256 of data using key.
func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// hmacSHA256Hex returns HMAC-SHA256 as hex string.
func hmacSHA256Hex(key []byte, data string) string {
	return hex.EncodeToString(hmacSHA256(key, data))
}

// getSignatureKey derives the signing key for SigV4.
func getSignatureKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")
	return kSigning
}
