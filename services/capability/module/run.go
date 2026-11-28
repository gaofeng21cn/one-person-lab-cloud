package module

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc"
	"opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/capability/catalog"
	"opl-cloud/services/capability/migrations"
	"opl-cloud/services/internal/ownerservice"
)

type httpServerCloser struct {
	server   *http.Server
	listener net.Listener
}

func (c *httpServerCloser) Close() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.server.Shutdown(shutdownCtx)
	return c.listener.Close()
}

// Run executes the Capability owner module until ctx is canceled.
func Run(ctx context.Context, env ownerservice.ModuleEnv) error {
	config, err := ownerservice.LoadConfig(env.Env, ownerservice.OwnerCapability, ":8181")
	if err != nil {
		return err
	}
	config.TLS.InProc = env.InProc

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	source, err := migrations.Source()
	if err != nil {
		return err
	}

	bootstrap, err := ownerservice.Start(runCtx, config, source, func(server *ownerservice.Server, database *ownerservice.Database) error {
		if database == nil {
			return ownerservice.ErrHandlersNotImplemented
		}
		schemaPath, schemaDigest := env.Env("OPL_CAPABILITY_PACKAGE_SCHEMA_PATH"), env.Env("OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST")
		if schemaPath == "" || schemaDigest == "" {
			return ownerservice.ErrHandlersNotImplemented
		}
		policy := catalog.UploadPolicy{MaxBytes: 1 << 30, PartBytes: 16 << 20, MaxExpandedBytes: 4 << 30, MaxFiles: 100000, TTL: 24 * time.Hour, ManifestPath: "manifest.json", SchemaPath: schemaPath, SchemaDigest: schemaDigest}
		objects, err := capabilityObjects(runCtx, env, policy)
		if err != nil {
			return err
		}
		tls := config.TLS
		runtimeConn, err := dialPeer(config, env.Env, tls, owneridentity.RuntimeControl, env.Env("OPL_RUNTIME_CONTROL_ADDR"))
		if err != nil {
			return err
		}
		if err := server.TrackCloser(runtimeConn); err != nil {
			runtimeConn.Close()
			return err
		}
		buildConn, err := dialPeer(config, env.Env, tls, owneridentity.Build, env.Env("OPL_BUILD_ADDR"))
		if err != nil {
			return err
		}
		if err := server.TrackCloser(buildConn); err != nil {
			buildConn.Close()
			return err
		}
		authorizer, idConn, err := ownerservice.AuthorizerFromConfig(config)
		if err != nil {
			return err
		}
		if idConn != nil {
			if err := server.TrackCloser(idConn); err != nil {
				idConn.Close()
				return err
			}
		}
		service, err := catalog.New(database.DB(), authorizer.Authorize, objects)
		if err != nil {
			return err
		}
		ledgerConn, err := dialPeer(config, env.Env, tls, owneridentity.Ledger, env.Env("OPL_LEDGER_ADDR"))
		if err != nil {
			return err
		}
		if err := server.TrackCloser(ledgerConn); err != nil {
			ledgerConn.Close()
			return err
		}
		if err := service.ConfigurePublisherSchema(env.Env("OPL_PUBLISHER_SCHEMA_PATH"), env.Env("OPL_PUBLISHER_SCHEMA_DIGEST")); err != nil {
			return err
		}
		service.LedgerInbox = api.NewDomainInboxClient(ledgerConn)
		service.Runtime = api.NewRuntimeControlProductServiceClient(runtimeConn)
		service.Build = api.NewBuildCoordinationClient(buildConn)
		service.Usage = api.NewClaimUsageReadbackClient(buildConn)
		service.Commit = api.NewOwnerCommitReadbackClient(buildConn)
		service.BuildInbox = api.NewDomainInboxClient(buildConn)
		if address := env.Env("OPL_SERVE_ADDR"); address != "" {
			serveConn, err := dialPeer(config, env.Env, tls, owneridentity.Serve, address)
			if err != nil {
				return err
			}
			if err = server.TrackCloser(serveConn); err != nil {
				serveConn.Close()
				return err
			}
			service.ServeCommit = api.NewOwnerCommitReadbackClient(serveConn)
			service.ServeUsage = api.NewClaimUsageReadbackClient(serveConn)
		}
		if err := service.Register(server); err != nil {
			return err
		}
		handler, err := service.DataHandler(env.Env("OPL_CAPABILITY_OBJECT_TOKEN"))
		if err != nil {
			return err
		}
		address := env.Env("OPL_CAPABILITY_OBJECT_LISTEN_ADDR")
		if address == "" {
			return fmt.Errorf("Capability object listener is required")
		}
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return err
		}
		dataServer := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
		if err := server.TrackCloser(&httpServerCloser{server: dataServer, listener: listener}); err != nil {
			listener.Close()
			return err
		}
		go func() {
			if err := dataServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Print("Capability data plane stopped unexpectedly")
				cancel()
			}
		}()
		go service.RunRegistrationDelivery(runCtx)
		return nil
	})
	if err != nil {
		return err
	}
	defer bootstrap.Close()
	return ownerservice.ServeContext(runCtx, bootstrap.Server)
}

func capabilityObjects(ctx context.Context, env ownerservice.ModuleEnv, policy catalog.UploadPolicy) (*catalog.Objects, error) {
	kind := strings.ToLower(env.Env("OPL_CAPABILITY_STORAGE_KIND"))
	if kind == "" {
		kind = "local"
	}
	switch kind {
	case "local":
		root, publicURL, signing := env.Env("OPL_CAPABILITY_OBJECT_ROOT"), env.Env("OPL_CAPABILITY_OBJECT_URL"), env.Env("OPL_CAPABILITY_OBJECT_SIGNING_KEY")
		if root == "" || publicURL == "" || len(signing) < 32 {
			return nil, fmt.Errorf("local storage requires object root, upload URL and a signing key of at least 32 bytes")
		}
		return catalog.NewObjects(root, publicURL, []byte(signing), policy)
	case "cos":
		store, err := catalog.NewCOSObjects(ctx, catalog.COSConfig{
			Bucket:    env.Env("OPL_CAPABILITY_COS_BUCKET"),
			Region:    env.Env("OPL_CAPABILITY_COS_REGION"),
			SecretID:  env.Env("OPL_CAPABILITY_COS_SECRET_ID"),
			SecretKey: env.Env("OPL_CAPABILITY_COS_SECRET_KEY"),
			Endpoint:  env.Env("OPL_CAPABILITY_COS_ENDPOINT"),
		}, policy)
		if err != nil {
			return nil, err
		}
		return store, nil
	default:
		return nil, fmt.Errorf("unsupported capability storage kind %q", kind)
	}
}

func dialPeer(config ownerservice.Config, getenv func(string) string, tls owneridentity.TLSConfig, target owneridentity.Owner, address string) (*grpc.ClientConn, error) {
	if address == "" {
		return nil, fmt.Errorf("%s address is required", target)
	}
	options, err := tls.DialOptions(config.Owner.Service(), owneridentity.DeploymentUnitTransportPrincipal(target), getenv("OPL_"+strings.ToUpper(target.String())+"_TOKEN"))
	if err != nil {
		return nil, err
	}
	return grpc.NewClient(address, options...)
}
