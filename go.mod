module github.com/haasonsaas/nexus

go 1.24.0

toolchain go1.24.12

require (
	github.com/anthropics/anthropic-sdk-go v1.19.0
	github.com/bwmarrin/discordgo v0.29.0
	github.com/firecracker-microvm/firecracker-go-sdk v1.0.0
	github.com/go-telegram/bot v1.17.0
	github.com/golang-jwt/jwt/v5 v5.3.0
	github.com/google/uuid v1.6.0
	// github.com/lancedb/lancedb-go v0.0.2 // Temporarily commented out due to version issue
	github.com/lib/pq v1.10.9
	github.com/mattn/go-sqlite3 v1.14.33
	github.com/playwright-community/playwright-go v0.5200.1
	github.com/prometheus/client_golang v1.23.2
	github.com/santhosh-tekuri/jsonschema/v5 v5.3.1
	github.com/sashabaranov/go-openai v1.41.2
	github.com/slack-go/slack v0.17.3
	github.com/spf13/cobra v1.10.2
	go.mau.fi/whatsmeow v0.0.0-20260121184854-c71785771e41
	go.opentelemetry.io/otel v1.39.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.39.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.39.0
	go.opentelemetry.io/otel/sdk v1.39.0
	go.opentelemetry.io/otel/trace v1.39.0
	golang.org/x/oauth2 v0.34.0
	google.golang.org/grpc v1.78.0
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.44.3
)

require (
	github.com/DATA-DOG/go-sqlmock v1.5.2
	github.com/chromedp/cdproto v0.0.0-20250724212937-08a3db8b4327
	github.com/chromedp/chromedp v0.14.2
	github.com/aws/aws-sdk-go-v2 v1.41.1
	github.com/aws/aws-sdk-go-v2/config v1.32.7
	github.com/aws/aws-sdk-go-v2/service/s3 v1.95.1
	github.com/fsnotify/fsnotify v1.9.0
	github.com/robfig/cron/v3 v3.0.1
	golang.org/x/image v0.35.0
	golang.org/x/text v0.33.0
	google.golang.org/genai v1.43.0
	maunium.net/go/mautrix v0.26.2
)

require (
	cloud.google.com/go v0.116.0 // indirect
	cloud.google.com/go/auth v0.9.3 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/PuerkitoBio/purell v1.1.1 // indirect
	github.com/PuerkitoBio/urlesc v0.0.0-20170810143723-de5bf2ad4578 // indirect
	github.com/asaskevich/govalidator v0.0.0-20210307081110-f21760c49a8d // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.4 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.7 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.17 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.17 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.17 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.0.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.13 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.41.6 // indirect
	github.com/aws/smithy-go v1.24.0 // indirect
	github.com/beeper/argo-go v1.1.2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/containerd/fifo v1.0.0 // indirect
	github.com/containernetworking/cni v1.0.1 // indirect
	github.com/containernetworking/plugins v1.0.1 // indirect
	github.com/deckarep/golang-set/v2 v2.7.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/elliotchance/orderedmap/v3 v3.1.0 // indirect
	github.com/go-jose/go-jose/v3 v3.0.4 // indirect
	github.com/go-json-experiment/json v0.0.0-20250725192818-e39067aee2d2 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/analysis v0.21.2 // indirect
	github.com/go-openapi/errors v0.20.2 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.19.6 // indirect
	github.com/go-openapi/loads v0.21.1 // indirect
	github.com/go-openapi/runtime v0.24.0 // indirect
	github.com/go-openapi/spec v0.20.4 // indirect
	github.com/go-openapi/strfmt v0.21.2 // indirect
	github.com/go-openapi/swag v0.21.1 // indirect
	github.com/go-openapi/validate v0.22.0 // indirect
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/s2a-go v0.1.8 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.4 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.3 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/petermattis/goid v0.0.0-20260113132338-7c7de50cc741 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rs/zerolog v1.34.0 // indirect
	github.com/sirupsen/logrus v1.8.3 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/vektah/gqlparser/v2 v2.5.27 // indirect
	github.com/vishvananda/netlink v1.1.1-0.20210330154013-f5de75959ad5 // indirect
	github.com/vishvananda/netns v0.0.0-20210104183010-2eb08e3e575f // indirect
	go.mau.fi/libsignal v0.2.1 // indirect
	go.mau.fi/util v0.9.5 // indirect
	go.mongodb.org/mongo-driver v1.8.3 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.39.0 // indirect
	go.opentelemetry.io/proto/otlp v1.9.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/crypto v0.47.0 // indirect
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	modernc.org/libc v1.67.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
