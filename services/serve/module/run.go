package module

import (
	"context"

	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/serve/internal/delivery"
	"opl-cloud/services/serve/migrations"
)

// Run executes the Serve owner module until ctx is canceled.
func Run(ctx context.Context, env ownerservice.ModuleEnv) error {
	config, err := ownerservice.LoadConfig(env.Env, ownerservice.OwnerServe, ":8185")
	if err != nil {
		return err
	}
	config.TLS.InProc = env.InProc

	source, err := migrations.Source()
	if err != nil {
		return err
	}

	bootstrap, err := ownerservice.Start(ctx, config, source, func(server *ownerservice.Server, database *ownerservice.Database) error {
		return delivery.ConfigureWithEnv(server, database, config, env.Env)
	})
	if err != nil {
		return err
	}
	defer bootstrap.Close()
	return ownerservice.ServeContext(ctx, bootstrap.Server)
}
