package goe

import (
	"context"
	"errors"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"go.oease.dev/goe/contracts"
	"go.oease.dev/goe/core"
	"go.oease.dev/goe/modules/broker"
	"go.oease.dev/goe/modules/config"
	"go.oease.dev/goe/modules/log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type App struct {
	configs     *core.GoeConfig
	container   *core.Container
	running     bool
	gracefulCtx context.Context
}

var appInstance *App

func NewApp() error {
	configModule := config.New("./configs")
	appEnv := configModule.GetOrDefaultString("APP_ENV", "dev")
	var logModule *log.Log
	if appEnv == "dev" {
		logModule = log.New(log.LevelDev)
	} else {
		logModule = log.New(log.LevelProd)
	}
	app := &App{}
	err := app.applyEnvConfig(configModule)
	if err != nil {
		return err
	}
	app.container = core.NewContainer(configModule, logModule, app.configs)
	appInstance = app

	// Initialize MongoDB
	if appInstance.configs.Features.MongoDBEnabled {
		appInstance.container.InitMongo()
	}

	// Initialize Meilisearch
	if appInstance.configs.Features.MeilisearchEnabled && appInstance.configs.Features.MongoDBEnabled {
		appInstance.container.InitMeilisearch()
	}

	// Init Queue
	appInstance.container.InitQueue()

	// Init Cron
	appInstance.container.InitCron()

	// Init Cache
	appInstance.container.InitCache()

	// Init Mailer
	if appInstance.configs.Features.MailerEnabled {
		appInstance.container.InitMailer()
	}

	// Init Fiber
	appInstance.container.InitFiber()

	// Init EMQX
	//appInstance.container.InitEMQX()

	return nil
}

// applyEnvConfig applies environment configuration to the App instance.
// It populates the configs field with values from the configModule parameter.
// It returns an error if there is an issue applying the configuration.
func (app *App) applyEnvConfig(configModule *config.Config) error {
	app.configs = &core.GoeConfig{
		App: &core.AppConfigs{
			Name:    configModule.GetOrDefaultString("APP_NAME", "GoeApp"),
			Version: configModule.GetOrDefaultString("APP_VERSION", "v1.0.0"),
			Env:     configModule.GetOrDefaultString("APP_ENV", "dev"),
		},
		Features: &core.GoeConfigFeatures{
			MongoDBEnabled:      configModule.GetOrDefaultBool("MONGODB_ENABLED", false),
			MeilisearchEnabled:  configModule.GetOrDefaultBool("MEILISEARCH_ENABLED", false),
			SearchDBSyncEnabled: configModule.GetOrDefaultBool("MEILISEARCH_DB_SYNC", false),
			MailerEnabled:       configModule.GetOrDefaultBool("MAILER_ENABLED", false),
			EMQXBrokerEnabled:   configModule.GetOrDefaultBool("EMQX_BROKER_ENABLED", false),
		},
		MongoDB: &core.GoeConfigMongodb{
			URI: configModule.GetOrDefaultString("MONGODB_URI", ""),
			DB:  configModule.GetOrDefaultString("MONGODB_DB", ""),
		},
		Redis: &core.GoeConfigRedis{
			Host:     configModule.GetOrDefaultString("REDIS_HOST", ""),
			Port:     configModule.GetOrDefaultInt("REDIS_PORT", 0),
			Username: configModule.GetOrDefaultString("REDIS_USERNAME", ""),
			Password: configModule.GetOrDefaultString("REDIS_PASSWORD", ""),
		},
		Meilisearch: &core.GoeConfigMeilisearch{
			Endpoint: configModule.GetOrDefaultString("MEILISEARCH_ENDPOINT", ""),
			ApiKey:   configModule.GetOrDefaultString("MEILISEARCH_API_KEY", ""),
		},
		Mailer: &core.GoeConfigMailer{
			Provider:  configModule.GetOrDefaultString("MAILER_PROVIDER", "smtp"),
			FromEmail: configModule.GetOrDefaultString("MAILER_FROM_EMAIL", ""),
			FromName:  configModule.GetOrDefaultString("MAILER_FROM_NAME", ""),
			SMTP: &core.GoeConfigSMTP{
				Host:       configModule.GetOrDefaultString("SMTP_HOST", ""),
				Port:       configModule.GetOrDefaultInt("SMTP_PORT", 0),
				Username:   configModule.GetOrDefaultString("SMTP_USERNAME", ""),
				Password:   configModule.GetOrDefaultString("SMTP_PASSWORD", ""),
				Tls:        configModule.GetOrDefaultBool("SMTP_TLS", false),
				LocalName:  configModule.GetOrDefaultString("SMTP_LOCAL_NAME", ""),
				AuthMethod: configModule.GetOrDefaultString("SMTP_AUTH_METHOD", "PLAIN"),
			},
			Resend: &core.GoeConfigResend{
				APIKey: configModule.GetOrDefaultString("RESEND_API_KEY", ""),
			},
			SES: &core.GoeConfigSES{
				Region:          configModule.GetOrDefaultString("SES_REGION", ""),
				AccessKeyID:     configModule.GetOrDefaultString("SES_ACCESS_KEY_ID", ""),
				SecretAccessKey: configModule.GetOrDefaultString("SES_SECRET_ACCESS_KEY", ""),
				Endpoint:        configModule.GetOrDefaultString("SES_ENDPOINT", ""),
			},
		},
		Queue: &core.GoeConfigQueue{
			ConcurrentWorkers:  configModule.GetOrDefaultInt("QUEUE_CONCURRENCY", 1),
			FetchInterval:      configModule.GetOrDefaultInt("QUEUE_FETCH_INTERVAL", 1),
			FetchLimit:         configModule.GetOrDefaultInt("QUEUE_FETCH_LIMIT", 0),
			MaxConsumeDuration: configModule.GetOrDefaultInt("QUEUE_MAX_CONSUME_DURATION", 5),
			DefaultRetries:     configModule.GetOrDefaultInt("QUEUE_DEFAULT_RETRIES", 3),
		},
		Http: &core.GoeConfigHttp{
			Port:            configModule.GetOrDefaultString("HTTP_PORT", "3000"),
			ServerHeader:    configModule.GetOrDefaultString("HTTP_SERVER_HEADER", "GoeAppServer/v1"),
			BodyLimit:       configModule.GetOrDefaultInt("HTTP_BODY_LIMIT", fiber.DefaultBodyLimit),
			Concurrency:     configModule.GetOrDefaultInt("HTTP_CONCURRENCY", fiber.DefaultConcurrency),
			ProxyHeader:     configModule.GetOrDefaultString("HTTP_PROXY_HEADER", ""),
			TrustProxyCheck: configModule.GetOrDefaultBool("HTTP_TRUSTED_PROXY_CHECK", false),
			TrustProxies:    configModule.GetStringSlice("HTTP_TRUSTED_PROXIES"),
			IPValidation:    configModule.GetOrDefaultBool("HTTP_IP_VALIDATION", false),
			ReduceMemory:    configModule.GetOrDefaultBool("HTTP_REDUCE_MEMORY", false),
		},
		Session: &core.GoeConfigSession{
			KeyLookup:  configModule.GetOrDefaultString("SESSION_LOOKUP", "cookie:goe_session_id"),
			Expiration: configModule.GetOrDefaultInt("SESSION_EXPIRATION", 86400),
		},
		S3: &core.GoeConfigS3{
			Endpoint:     configModule.GetOrDefaultString("S3_ENDPOINT", ""),
			AccessKey:    configModule.GetOrDefaultString("S3_ACCESS_KEY", ""),
			SecretKey:    configModule.GetOrDefaultString("S3_SECRET_KEY", ""),
			Bucket:       configModule.GetOrDefaultString("S3_BUCKET_NAME", ""),
			Region:       configModule.GetOrDefaultString("S3_REGION", ""),
			BucketLookup: configModule.GetOrDefaultString("S3_BUCKET_LOOKUP", "path"),
			UseSSL:       configModule.GetOrDefaultBool("S3_USE_SSL", false),
			Token:        configModule.GetOrDefaultString("S3_TOKEN", ""),
		},
		OIDC: &core.GoeOIDCConfig{
			AppId:     configModule.Get("OIDC_APP_ID"),
			AppSecret: configModule.Get("OIDC_APP_SECRET"),
			AppScopes: configModule.GetStringSlice("OIDC_APP_SCOPES"),
			Issuer:    configModule.Get("OIDC_ISSUER"),
		},
		EMQX: &broker.EMQXConfig{
			ID:       configModule.GetOrDefaultString("EMQX_HOST", uuid.NewString()),
			Addr:     configModule.GetOrDefaultString("EMQX_ADDR", "tcp://localhost:1883"),
			Username: configModule.GetOrDefaultString("EMQX_USERNAME", "admin"),
			Password: configModule.GetOrDefaultString("EMQX_PASSWORD", "public"),
			TLSConfig: &broker.TLSConfig{
				Enable:   configModule.GetOrDefaultBool("EMQX_TLS_ENABLED", false),
				CA:       configModule.GetOrDefaultString("EMQX_TLS_CA", "ca.pem"),
				CertFile: configModule.GetOrDefaultString("EMQX_TLS_CERT_FILE", "client-crt.pem"),
				KeyFile:  configModule.GetOrDefaultString("EMQX_TLS_KEY_FILE", "client-key.pem"),
			},
		},
	}
	return nil
}

func UseDB() contracts.MongoDB {
	if appInstance == nil {
		panic("must initialize App first, by calling NewApp() method")
		return nil
	}
	return appInstance.container.GetMongo()
}

func UseCron() contracts.CronJob {
	if appInstance == nil {
		panic("must initialize App first, by calling NewApp() method")
		return nil
	}
	return appInstance.container.GetCron()
}

func UseLog() contracts.Logger {
	if appInstance == nil {
		panic("must initialize App first, by calling NewApp() method")
		return nil
	}
	return appInstance.container.GetLogger()
}

func UseCfg() contracts.Config {
	if appInstance == nil {
		panic("must initialize App first, by calling NewApp() method")
		return nil
	}
	return appInstance.container.GetConfig()
}

func UseMQ() contracts.Queue {
	if appInstance == nil {
		panic("must initialize App first, by calling NewApp() method")
		return nil
	}
	return appInstance.container.GetQueue()
}

func UseCache() contracts.Cache {
	if appInstance == nil {
		panic("must initialize App first, by calling NewApp() method")
		return nil
	}
	return appInstance.container.GetCache()
}

func UseSearch() contracts.Meilisearch {
	if appInstance == nil {
		panic("must initialize App first, by calling NewApp() method")
		return nil
	}
	return appInstance.container.GetMeilisearch()
}

func UseMailer() contracts.Mailer {
	if appInstance == nil {
		panic("must initialize App first, by calling NewApp() method")
		return nil
	}
	return appInstance.container.GetMailer()
}

func UseFiber() contracts.GoeFiber {
	if appInstance == nil {
		panic("must initialize App first, by calling NewApp() method")
		return nil
	}
	return appInstance.container.GetFiber()
}

func UseEMQX() contracts.EMQX {
	if appInstance == nil {
		panic("must initialize App first, by calling NewApp() method")
		return nil
	}
	return appInstance.container.GetEMQX()
}

func Run() error {
	if appInstance == nil {
		return errors.New("must initialize App first, by calling NewApp() method")
	}
	if appInstance.running {
		return errors.New("app is already running")
	}
	go func() {
		err := appInstance.container.GetFiber().App().Listen(":"+appInstance.configs.Http.Port, fiber.ListenConfig{
			DisableStartupMessage: true,
			EnablePrefork:         false,
			EnablePrintRoutes:     false,
			OnShutdownError: func(err error) {
				appInstance.container.GetLogger().Error("Shutdown error: ", err)
			},
		})
		if err != nil {
			appInstance.running = false
			appInstance.container.GetLogger().Panic("Server error: ", err)
		}
	}()
	appInstance.running = true
	newShutdownHook().Close(func() {
		appInstance.running = false
		_ = appInstance.container.GetFiber().App().ShutdownWithTimeout(5 * time.Second)
		//if err != nil {
		//	appInstance.container.GetLogger().Error("Server shutdown error: ", err)
		//}
		appInstance.container.GetLogger().Info("Server has shutdown successfully!")
	})
	return nil
}

func AddShutdownHook(hookHandlers ...func() error) error {
	if appInstance == nil {
		return errors.New("must initialize App first, by calling NewApp() method")
	}
	if appInstance.running {
		return errors.New("app is already running, shutdown hook must be added before calling Run()")
	}
	appInstance.container.GetFiber().App().Hooks().OnShutdown(hookHandlers...)
	return nil
}

// thanks to https://github.com/xinliangnote/go-gin-api for the shutdown hook implementation
var _ hook = (*sdhook)(nil)

// Hook a graceful shutdown hook, default with signals of SIGINT and SIGTERM
type hook interface {
	// WithSignals add more signals into hook
	WithSignals(signals ...syscall.Signal) hook

	// Close register shutdown handles
	Close(funcs ...func())
}
type sdhook struct {
	ctx chan os.Signal
}

// NewHook create a Hook instance
func newShutdownHook() hook {
	hook := &sdhook{
		ctx: make(chan os.Signal, 1),
	}

	return hook.WithSignals(syscall.SIGINT, syscall.SIGTERM)
}
func (h *sdhook) WithSignals(signals ...syscall.Signal) hook {
	for _, s := range signals {
		signal.Notify(h.ctx, s)
	}

	return h
}
func (h *sdhook) Close(funcs ...func()) {
	select {
	case <-h.ctx:
	}
	signal.Stop(h.ctx)

	for _, f := range funcs {
		f()
	}
}
