package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/go-cleanhttp"
)

const (
	headerAccept             = "Accept"
	headerAuthorization      = "Authorization"
	headerContentDisposition = "Content-Disposition"
	headerContentType        = "Content-Type"

	applicationVNDAPIJSON = "application/vnd.api+json"
	applicationJSON       = "application/json"
	binaryOctetStream     = "binary/octet-stream"
	attachmentFilenameFmt = "attachment; filename=%q"

	pathAPI               = "api"
	pathOrganizations     = "organizations"
	pathPlatforms         = "platforms"
	pathRegistryProviders = "registry-providers"
	pathVersions          = "versions"
	pathV2                = "v2"
)

type tfClientMiddleware struct {
	addr        string
	bearerToken string
	hc          *http.Client
}

func newTFClientMiddleware(cfg *Config) (*tfClientMiddleware, error) {
	tm := new(tfClientMiddleware)
	tm.addr = strings.Trim(cfg.TFAddress, "/")
	tm.bearerToken = cfg.TFToken
	tm.hc = cleanhttp.DefaultClient()

	return tm, nil
}

// copy does a shallow copy of the middleware, overriding the http client with the one provided.  used by the
// TFUploadsClient.
func (tm *tfClientMiddleware) copy(hc *http.Client) *tfClientMiddleware {
	tm2 := new(tfClientMiddleware)
	*tm2 = *tm
	tm2.hc = hc
	return tm2
}

func (tm *tfClientMiddleware) buildURL(route string) string {
	return fmt.Sprintf("%s/%s", tm.addr, route)
}

func (tm *tfClientMiddleware) buildRequest(ctx context.Context, method, routePath string, query url.Values, body interface{}) (*http.Request, error) {
	var bodyRdr io.Reader

	// handle different incoming body types
	if body != nil {
		switch body.(type) {
		case io.Reader:
			bodyRdr = body.(io.Reader)
		case []byte:
			bodyRdr = bytes.NewBuffer(body.([]byte))
		default:
			if b, err := json.Marshal(body); err != nil {
				return nil, fmt.Errorf("error marshalling body: %w", err)
			} else {
				bodyRdr = bytes.NewBuffer(b)
			}
		}
	}

	compiledURL := tm.buildURL(routePath)
	if len(query) > 0 {
		compiledURL = fmt.Sprintf("%s?%s", compiledURL, query.Encode())
	}

	r, err := http.NewRequestWithContext(ctx, method, compiledURL, bodyRdr)

	return r, err
}

func (tm *tfClientMiddleware) do(r *http.Request) (*http.Response, error) {
	// todo: this abstraction is here as i plan to eventually move additional logic here.
	return tm.hc.Do(r)
}

type TFClient struct {
	m *tfClientMiddleware
}

func NewTFClient(cfg *Config) (*TFClient, error) {
	var (
		err error

		tc = new(TFClient)
	)

	if tc.m, err = newTFClientMiddleware(cfg); err != nil {
		return nil, err
	}

	return tc, nil
}

type TFProviderClient struct {
	m *tfClientMiddleware
}

func (tc *TFClient) ProviderClient() *TFProviderClient {
	return &TFProviderClient{m: tc.m}
}

func (tc *TFProviderClient) CreateProviderVersion(
	ctx context.Context,
	orgName,
	regName,
	namespace,
	provName string,
	data TFCreateProviderVersionRequest,
) (*TFCreateProviderVersionResponse, error) {
	route := buildRoute(
		pathAPI,
		pathV2,
		pathOrganizations,
		orgName,
		pathRegistryProviders,
		regName,
		namespace,
		provName,
		pathVersions,
	)
	req, err := tc.m.buildRequest(ctx, http.MethodPost, route, nil, data)
	if err != nil {
		return nil, err
	}
	setBearerToken(req, tc.m.bearerToken)
	req.Header.Set(headerContentType, applicationVNDAPIJSON)
	req.Header.Set(headerAccept, applicationJSON)
	resp, err := tc.m.do(req)
	out := TFCreateProviderVersionResponse{}
	if err = handleResponse(req, resp, err, &out, http.StatusCreated); err != nil {
		return nil, err
	}
	return &out, nil
}

func (tc *TFProviderClient) CreateProviderVersionPlatform(
	ctx context.Context,
	orgName,
	regName,
	namespace,
	provName,
	provVersion string,
	data TFCreateProviderVersionPlatformRequest,
) (*TFCreateProviderVersionPlatformResponse, error) {
	route := buildRoute(
		pathAPI,
		pathV2,
		pathOrganizations,
		orgName,
		pathRegistryProviders,
		regName,
		namespace,
		provName,
		pathVersions,
		provVersion,
		pathPlatforms,
	)
	req, err := tc.m.buildRequest(ctx, http.MethodPost, route, nil, data)
	if err != nil {
		return nil, err
	}
	setBearerToken(req, tc.m.bearerToken)
	req.Header.Set(headerContentType, applicationVNDAPIJSON)
	req.Header.Set(headerAccept, applicationJSON)
	resp, err := tc.m.do(req)
	out := TFCreateProviderVersionPlatformResponse{}
	if err = handleResponse(req, resp, err, &out, http.StatusCreated); err != nil {
		return nil, err
	}
	return &out, nil
}

type TFUploadsClient struct {
	m *tfClientMiddleware
}

// UploadsClient is the client to use for uploading checksum and artifact files to tf cloud
func (tc *TFClient) UploadsClient() *TFUploadsClient {
	return &TFUploadsClient{m: tc.m.copy(cleanhttp.DefaultClient())}
}

func (tc *TFUploadsClient) UploadFile(ctx context.Context, data TFFileUploadRequest) error {
	req, err := tc.m.buildRequest(ctx, http.MethodPut, data.Destination, nil, data.File)
	if err != nil {
		return err
	}
	req.Header.Set(headerContentType, data.ContentType)
	req.Header.Set(headerContentDisposition, fmt.Sprintf(attachmentFilenameFmt, data.Filename))
	resp, err := tc.m.do(req)
	return handleResponse(req, resp, err, nil, http.StatusOK)
}
