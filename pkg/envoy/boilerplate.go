package envoy

import (
	"log"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	fal "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v2"
	al "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	hcm "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/types"
)

func makeVirtualHost(host string, timeout time.Duration) route.VirtualHost {
	virtualHost := route.VirtualHost{
		Name:    "local_service",
		Domains: []string{host},
		Routes: []route.Route{route.Route{
			Match: route.RouteMatch{
				PathSpecifier: &route.RouteMatch_Prefix{
					Prefix: "/",
				},
			},
			Action: &route.Route_Route{
				Route: &route.RouteAction{
					ClusterSpecifier: &route.RouteAction_Cluster{
						Cluster: host,
					},
					RetryPolicy: &route.RouteAction_RetryPolicy{
						RetryOn:       "5xx",
						PerTryTimeout: &timeout,
					},
				},
			},
		}},
	}
	return virtualHost
}

func makeConnectionManager(virtualHosts []route.VirtualHost) *hcm.HttpConnectionManager {
	accessLogConfig, err := util.MessageToStruct(&fal.FileAccessLog{Path: "/var/log/envoy/test.log"})
	if err != nil {
		log.Fatalf("failed to convert: %s", err)
	}
	return &hcm.HttpConnectionManager{
		CodecType:  hcm.AUTO,
		StatPrefix: "ingress_http",
		HttpFilters: []*hcm.HttpFilter{&hcm.HttpFilter{
			Name: "envoy.router",
		}},
		RouteSpecifier: &hcm.HttpConnectionManager_RouteConfig{
			RouteConfig: &v2.RouteConfiguration{
				Name:         "local_route",
				VirtualHosts: virtualHosts,
			},
		},
		Tracing: &hcm.HttpConnectionManager_Tracing{
			OperationName: hcm.EGRESS,
		},
		AccessLog: []*al.AccessLog{
			{
				Name:   "envoy.file_access_log",
				Config: accessLogConfig,
			},
		},
	}
}

func makeListener(virtualHosts []route.VirtualHost, cert, key string) []cache.Resource {
	httpConnectionManager := makeConnectionManager(virtualHosts)
	httpConfig, err := util.MessageToStruct(httpConnectionManager)
	if err != nil {
		log.Fatalf("failed to convert: %s", err)
	}

	tls := &auth.DownstreamTlsContext{}
	if cert != "" && key != "" {
		tls.CommonTlsContext = &auth.CommonTlsContext{
			TlsCertificates: []*auth.TlsCertificate{
				&auth.TlsCertificate{
					CertificateChain: &core.DataSource{
						Specifier: &core.DataSource_Filename{Filename: cert},
					},
					PrivateKey: &core.DataSource{
						Specifier: &core.DataSource_Filename{Filename: key},
					},
				},
			},
		}
	} else {
		tls = nil
	}

	listener := v2.Listener{
		Name: "listener_0",
		Address: core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Address: "0.0.0.0",
					PortSpecifier: &core.SocketAddress_PortValue{
						PortValue: 10000,
					},
				},
			},
		},
		FilterChains: []listener.FilterChain{listener.FilterChain{
			TlsContext: tls,
			Filters: []listener.Filter{listener.Filter{
				Name:   "envoy.http_connection_manager",
				Config: httpConfig,
			}},
		}},
	}

	return []cache.Resource{&listener}
}

func makeAddresses(addresses []string) []*core.Address {

	envoyAddresses := []*core.Address{}
	for _, address := range addresses {
		envoyAddress := &core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Address: address,
					PortSpecifier: &core.SocketAddress_PortValue{
						PortValue: 443,
					},
				},
			},
		}
		envoyAddresses = append(envoyAddresses, envoyAddress)
	}

	return envoyAddresses
}

func makeCluster(host, ca, healthPath string, addresses []*core.Address) *v2.Cluster {

	tls := &auth.UpstreamTlsContext{}
	if ca != "" {
		tls.CommonTlsContext = &auth.CommonTlsContext{
			ValidationContextType: &auth.CommonTlsContext_ValidationContext{
				ValidationContext: &auth.CertificateValidationContext{
					TrustedCa: &core.DataSource{
						Specifier: &core.DataSource_Filename{Filename: ca},
					},
				},
			},
		}
	} else {
		tls = nil
	}
	cluster := &v2.Cluster{
		Type:           v2.Cluster_STRICT_DNS,
		Name:           host,
		ConnectTimeout: time.Second * 30,
		Hosts:          addresses,
		TlsContext:     tls,
		HealthChecks: []*core.HealthCheck{&core.HealthCheck{
			Timeout:            &types.Duration{Seconds: 5},
			Interval:           &types.Duration{Seconds: 10},
			UnhealthyThreshold: &types.UInt32Value{Value: 3},
			HealthyThreshold:   &types.UInt32Value{Value: 3},
			HealthChecker: &core.HealthCheck_HttpHealthCheck_{
				HttpHealthCheck: &core.HealthCheck_HttpHealthCheck{
					Path: healthPath,
				},
			},
		}},
	}
	return cluster
}
