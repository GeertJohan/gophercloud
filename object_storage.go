package gophercloud

import (
	"github.com/racker/perigee"
	"strings"
)

const (
	ContainerMetadataPrefix = "x-container-meta-"
)

// containerMetaName takes an unadorned custom metadata key and formats it suitably for map
// look-up.
func containerMetaName(s string) string {
	return strings.ToLower(ContainerMetadataPrefix + s)
}

// The openstackObjectStorageProvider structure provides the implementation for generic OpenStack-compatible
// object storage interfaces.
type openstackObjectStoreProvider struct {
	// endpoint refers to the provider's API endpoint base URL.  This will be used to construct
	// and issue queries.
	endpoint string

	// Test context (if any) in which to issue requests.
	context *Context

	// access associates this API provider with a set of credentials,
	// which may be automatically renewed if they near expiration.
	access AccessProvider
}

// openstackContainer provides the backing state required to keep track of a single container in an OpenStack
// environment.
type openstackContainer struct {
	// Name labels the container.
	Name string

	// Provider links the container to an actual provider.
	Provider *openstackObjectStoreProvider

	// customMetadata provides access to the custom metadata for this container.
	customMetadata *cimap
}

// openstackContainerInfo holds the information describing a single OpenStack container.
type openstackContainerInfo struct {
	// Bytes is the the size of the container.
	Bytes int
	// Count is the number of objects in the container.
	Count int
	// Name is the label for the container.
	Name string
}

func (osp *openstackObjectStoreProvider) CreateContainer(name string) (Container, error) {
	var container Container

	err := osp.context.WithReauth(osp.access, func() error {
		url := osp.endpoint + "/" + name
		err := perigee.Put(url, perigee.Options{
			CustomClient: osp.context.httpClient,
			MoreHeaders: map[string]string{
				"X-Auth-Token": osp.access.AuthToken(),
			},
			OkCodes: []int{201},
		})
		if err == nil {
			container = &openstackContainer{
				Name:     name,
				Provider: osp,
			}
		}
		return err
	})
	return container, err
}

func (osp *openstackObjectStoreProvider) ListContainers() ([]ContainerInfo, error) {
	var osci []openstackContainerInfo
	err := osp.context.WithReauth(osp.access, func() error {
		url := osp.endpoint
		_, err := perigee.Request("GET", url, perigee.Options{
			CustomClient: osp.context.httpClient,
			Results:      &osci,
			MoreHeaders: map[string]string{
				"X-Auth-Token": osp.access.AuthToken(),
			},
		})
		return err
	})
	ci := make([]ContainerInfo, len(osci))
	for i, val := range osci {
		ci[i] = val
	}
	return ci, err
}

func (osp *openstackObjectStoreProvider) DeleteContainer(name string) error {
	err := osp.context.WithReauth(osp.access, func() error {
		url := osp.endpoint + "/" + name
		return perigee.Delete(url, perigee.Options{
			CustomClient: osp.context.httpClient,
			MoreHeaders: map[string]string{
				"X-Auth-Token": osp.access.AuthToken(),
			},
			OkCodes: []int{204},
		})
	})
	return err
}

func (c *openstackContainer) Delete() error {
	return c.Provider.DeleteContainer(c.Name)
}

func (c *openstackContainer) Metadata() (MetadataProvider, error) {
	// As of this writing, we let the openstackContainer structure keep track of its own metadata.
	return c, nil
}

// cacheHeaders() takes no action if custom metadata headers have already been retrieved.
// Otherwise, the container resource is queried for its current set of custom headers.
func (c *openstackContainer) cacheHeaders() error {
	osp := c.Provider
	return osp.context.WithReauth(osp.access, func() error {
		if c.customMetadata == nil {
			// Grab the set of headers attached to this container.
			// These headers will be keyed off of mixed-case strings.
			url := osp.endpoint + "/" + c.Name
			resp, err := perigee.Request("HEAD", url, perigee.Options{
				CustomClient: osp.context.httpClient,
				MoreHeaders: map[string]string{
					"X-Auth-Token": osp.access.AuthToken(),
				},
				OkCodes: []int{204},
			})
			if err != nil {
				return err
			}

			// To ensure case insensitivity when looking up keys,
			// transcribe our headers such that all the keys used to
			// index them are lower-case.
			headers := resp.HttpResponse.Header
			loweredHeaders := make(map[string]string)
			for key, values := range headers {
				key = strings.ToLower(key)
				if strings.HasPrefix(key, containerMetaName("")) {
					loweredHeaders[key[len(ContainerMetadataPrefix):]] = values[0]
				}
			}
			c.customMetadata = &cimap{m: loweredHeaders}
		}
		return nil
	})
}

// See MetadataProvider interface for details.
func (c *openstackContainer) CustomValues() (map[string]string, error) {
	err := c.cacheHeaders()
	if err != nil {
		return nil, err
	}
	return c.customMetadata.rawMap(), nil
}

// See MetadataProvider interface for details.
func (c *openstackContainer) CustomValue(key string) (string, error) {
	err := c.cacheHeaders()
	if err != nil {
		return "", err
	}
	key = strings.ToLower(key)
	value, _ := c.customMetadata.get(key)
	if len(value) > 0 {
		return value, nil
	}
	return "", nil
}

// See MetadataProvider interface for details.
func (c *openstackContainer) SetCustomValue(key, value string) error {
	osp := c.Provider
	err := osp.context.WithReauth(osp.access, func() error {
		url := osp.endpoint + "/" + c.Name
		_, err := perigee.Request("POST", url, perigee.Options{
			CustomClient: osp.context.httpClient,
			MoreHeaders: map[string]string{
				"X-Auth-Token":         osp.access.AuthToken(),
				containerMetaName(key): value,
			},
			OkCodes: []int{204},
		})
		return err
	})

	// Flush our values cache to make sure our next attempt at getting values always gets the right data.
	if err == nil {
		c.customMetadata = nil
	}

	return err
}

// See ContainerInfo interface for details
func (ci openstackContainerInfo) Label() string {
	return ci.Name
}

// See ContainerInfo interface for details
func (ci openstackContainerInfo) ObjCount() int {
	return ci.Count
}

// See ContainerInfo interface for details
func (ci openstackContainerInfo) Size() int {
	return ci.Bytes
}
