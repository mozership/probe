# 環境変数リファレンス

このページでは、Probeの動作、設定、実行を制御するすべての環境変数の包括的なドキュメントを提供します。

## 概要

Probeは以下のために環境変数を使用します：

- **ランタイム設定** - ログ、タイムアウト、動作の制御
- **認証** - APIキー、トークン、認証情報
- **統合** - CI/CDシステム、監視ツール
- **カスタマイズ** - 上限、デフォルト設定

環境変数はシステムレベル、CI/CDパイプライン、またはワークフローファイル内の`vars`セクションで定義できます。

## ランタイム設定変数

Probe自身が読む環境変数です。

### `PROBE_OUTPUT`

**型:** String  
**値:** `auto`, `spinner`, `stream`  
**デフォルト:** `auto`  
**説明:** レポートの出力方法を選びます。`auto`は対話的な端末なら`spinner`、それ以外なら`stream`を選びます。`--output`フラグを指定した場合はそちらが優先されます。

```bash
# 完了したものから順に出力する。CI のログ向け
export PROBE_OUTPUT=stream
probe workflow.yml
```

### `PROBE_REPORT`

**型:** String  
**値:** `format[=path]`をカンマで区切った並び。`format`は`json`、`junit`、`markdown`、`github-summary`のいずれか  
**デフォルト:** なし  
**説明:** 実行が終わったあとに書き出すレポートファイル。パスを省いた形式は既定のファイルに書き出します。`--report`フラグを指定した場合はそちらが優先されます。

```bash
# CI が読み取れるように JUnit XML を残す
export PROBE_REPORT=junit=out/junit.xml
probe workflow.yml
```

### `PROBE_MAX_REPEAT_COUNT`

**型:** Integer  
**デフォルト:** `10000`  
**説明:** ステップの`repeat.count`の上限です。これを超える指定は実行前にエラーになります。

```bash
export PROBE_MAX_REPEAT_COUNT=50000
probe load-test.yml
```

### `PROBE_MAX_ATTEMPTS`

**型:** Integer  
**デフォルト:** `10000`  
**説明:** ステップのリトライ`max_attempts`の上限です。

```bash
export PROBE_MAX_ATTEMPTS=100
probe workflow.yml
```

### `FORCE_COLOR`

**型:** String  
**値:** `1`  
**説明:** 標準出力が端末でない場合でも色付き出力を強制します。

```bash
FORCE_COLOR=1 probe workflow.yml | tee run.log
```

このページのこれ以降で扱うのは、利用者が定義し、ワークフローの`vars`から参照する環境変数です。

## 認証変数

認証情報はワークフローファイルに書くものではありません。環境変数に置き、ワークフローからは`env`経由で参照します。

### API認証

ワークフローでのAPI認証の一般的なパターン：

#### `API_TOKEN` / `API_KEY`

ベアラトークンは一度エクスポートしておき、ワークフローのauthorizationヘッダーから参照します。

```bash
# Bearerトークン認証
export API_TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

```yaml
# workflow.yml
vars:
  api_token: "{{API_TOKEN}}"

jobs:
- name: default
  defaults:
    http:
      headers:
        Authorization: "Bearer {{vars.api_token}}"
```

#### `USERNAME` / `PASSWORD`

Basic認証には両方の値が必要です。どちらもワークフローファイルには書きません。

```bash
# Basic認証資格情報
export API_USERNAME="admin"
export API_PASSWORD="secret123"
```

```yaml
# workflow.yml
vars:
  username: "{{API_USERNAME}}"
  password: "{{API_PASSWORD}}"

steps:
  - name: "Authenticated Request"
    uses: http
    with:
      method: GET
      url: "https://api.example.com/protected"
      headers:
        Authorization: "Basic {{base64(vars.username + ':' + vars.password)}}"
```

### メール認証

メール関連のステップには、サーバー、ポート、認証に使うアカウントが必要です。

#### SMTP設定

メール関連のステップには、ホスト、ポート、認証に使うアカウントが必要です。

```bash
# Gmailでアプリパスワード使用
export SMTP_HOST="smtp.gmail.com"
export SMTP_PORT=587
export SMTP_USERNAME="alerts@example.com"
export SMTP_PASSWORD="app-specific-password"

# Office 365
export SMTP_HOST="smtp.office365.com"
export SMTP_PORT=587
export SMTP_USERNAME="alerts@company.com"
export SMTP_PASSWORD="account-password"
```

```yaml
# workflow.yml
vars:
  smtp_host: "{{SMTP_HOST}}"
  smtp_port: "{{SMTP_PORT}}"
  smtp_username: "{{SMTP_USERNAME}}"
  smtp_password: "{{SMTP_PASSWORD}}"

jobs:
- name: default
  defaults:
    smtp:
      host: "{{vars.smtp_host}}"
      port: "{{vars.smtp_port}}"
      username: "{{vars.smtp_username}}"
      password: "{{vars.smtp_password}}"
      from: "{{vars.smtp_username}}"
```

## アプリケーション設定変数

デプロイ先ごとに変わる値、たとえばワークフローが対象とするアドレスや、有効になっているはずの機能をここで扱います。

### サービスURL

接続先は環境ごとに変わります。そのためワークフローには直接書かず、環境変数から読みます。

```bash
# APIエンドポイント
export API_BASE_URL="https://api.production.com"
export API_VERSION="v2"
export HEALTH_CHECK_URL="${API_BASE_URL}/${API_VERSION}/health"

# データベース接続
export DATABASE_URL="postgresql://user:pass@localhost:5432/db"
export REDIS_URL="redis://localhost:6379/0"

# 外部サービス
export SLACK_WEBHOOK_URL="https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"
export MONITORING_URL="https://monitoring.example.com/api/alerts"
```

### 機能フラグ

フラグを使えば、実行環境に当てはまらない確認を1つのワークフローの中で飛ばせます。

```bash
# 機能の有効/無効
export ENABLE_MONITORING=true
export ENABLE_SLACK_NOTIFICATIONS=false
export ENABLE_DETAILED_LOGGING=true
export ENABLE_PERFORMANCE_TRACKING=true
```

```yaml
# workflow.yml
vars:
  enable_monitoring: "{{ENABLE_MONITORING}}"
  enable_performance_tracking: "{{ENABLE_PERFORMANCE_TRACKING}}"
  enable_slack_notifications: "{{ENABLE_SLACK_NOTIFICATIONS}}"
  api_base_url: "{{API_BASE_URL}}"
  slack_webhook_url: "{{SLACK_WEBHOOK_URL}}"

jobs:
- name: monitoring
  steps:
    - name: "Performance Check"
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/metrics"

- name: notifications
  needs: [monitoring]
  steps:
    - name: "Slack Alert"
      uses: http
      with:
        url: "{{vars.slack_webhook_url}}"
        method: "POST"
        body: |
          {
          }
```

## CI/CD統合変数

CIはブランチ、コミット、実行IDをあらかじめ環境変数として公開しています。ワークフローからこれらを読めば、結果にラベルを付けたり、実行内容を切り替えたりできます。

### GitHub Actions

シークレットをステップの環境変数に渡せば、実行自体は1つのコマンドで済みます。

```yaml
# .github/workflows/probe.yml
name: API Tests
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Run Probe Tests
        env:
          API_TOKEN: ${{ secrets.API_TOKEN }}
          ENVIRONMENT: ${{ github.ref == 'refs/heads/main' && 'production' || 'staging' }}
          BRANCH_NAME: ${{ github.ref_name }}
          COMMIT_SHA: ${{ github.sha }}
          RUN_ID: ${{ github.run_id }}
        run: probe workflow.yml
```

**ワークフローで利用可能:**

```bash
export GITHUB_REPOSITORY="owner/repo"
export GITHUB_REF="refs/heads/main"
export GITHUB_SHA="abc123def456"
export GITHUB_RUN_ID="123456789"
export GITHUB_ACTOR="username"
```

### GitLab CI

プロジェクトの変数がジョブの環境変数に入るため、スクリプトは1行のままにできます。

```yaml
# .gitlab-ci.yml
probe-test:
  stage: test
  script:
    - probe workflow.yml
  variables:
    API_TOKEN: $API_TOKEN
    ENVIRONMENT: $CI_ENVIRONMENT_NAME
    BRANCH_NAME: $CI_COMMIT_REF_NAME
    COMMIT_SHA: $CI_COMMIT_SHA
    PIPELINE_ID: $CI_PIPELINE_ID
```

**ワークフローで利用可能:**

```bash
export CI_PROJECT_NAME="project-name"
export CI_COMMIT_REF_NAME="main"
export CI_COMMIT_SHA="abc123"
export CI_PIPELINE_ID="123456"
export CI_JOB_ID="789012"
```

### Jenkins

`environment`ブロックで、各認証情報をJenkinsのクレデンシャルストアから取得します。

```groovy
// Jenkinsfile
pipeline {
    agent any
    environment {
        API_TOKEN = credentials('api-token')
        ENVIRONMENT = "${BRANCH_NAME == 'main' ? 'production' : 'staging'}"
        BUILD_NUMBER = "${BUILD_NUMBER}"
        JOB_NAME = "${JOB_NAME}"
    }
    stages {
        stage('Test') {
            steps {
                sh 'probe workflow.yml'
            }
        }
    }
}
```

**ワークフローで利用可能:**

```bash
export BUILD_NUMBER="123"
export JOB_NAME="api-tests"
export WORKSPACE="/var/jenkins_home/workspace/api-tests"
export JENKINS_URL="https://jenkins.example.com"
```

## 環境固有設定

1つのワークフローを開発、ステージング、本番のそれぞれに対して動かす場面はよくあります。以下ではファイルを複製せずに環境ごとの値を選ぶ方法を示します。

### マルチ環境セットアップ

全環境で共通の値は1度だけ設定し、環境ごとに異なる値だけを切り替えます。

```bash
# ベース設定（常に設定）
export API_VERSION="v1"
export DEFAULT_TIMEOUT="30s"

# 環境固有（環境ごとに設定）
case $ENVIRONMENT in
  "production")
    export API_BASE_URL="https://api.prod.com"
    export DATABASE_URL="postgresql://prod-db:5432/app"
    export LOG_LEVEL="warn"
    export ENABLE_MONITORING=true
    ;;
  "staging")  
    export API_BASE_URL="https://api.staging.com"
    export DATABASE_URL="postgresql://staging-db:5432/app"
    export LOG_LEVEL="info"
    export ENABLE_MONITORING=true
    ;;
  "development")
    export API_BASE_URL="http://localhost:3000"
    export DATABASE_URL="postgresql://localhost:5432/app_dev"
    export LOG_LEVEL="debug"
    export ENABLE_MONITORING=false
    ;;
esac
```

### Docker設定

Probeは単一のバイナリなので、イメージ側で行うのは取得と、実行時に環境変数を渡すことだけです。

```dockerfile
# Dockerfile
FROM alpine:latest

# Probeをインストール
RUN apk add --no-cache curl && \
    curl -L https://github.com/mozership/probe/releases/latest/download/probe_linux_x86_64.tar.gz | tar -xz -C /usr/local/bin probe

# デフォルト環境変数を設定
ENV PROBE_OUTPUT=stream

COPY workflows/ /workflows/
WORKDIR /workflows

ENTRYPOINT ["probe"]
CMD ["workflow.yml"]
```

```yaml
# docker-compose.yml
version: '3.8'

services:
  probe:
    build: .
    environment:
      - API_TOKEN=${API_TOKEN}
      - API_BASE_URL=https://api.example.com
      - ENVIRONMENT=production
      - PROBE_OUTPUT=stream
    volumes:
      - ./workflows:/workflows
      - ./reports:/reports
    command: monitoring.yml
```

## セキュリティの考慮事項

環境変数を使えば秘密情報をワークフローファイルから外せますが、プロセスや、その内容を参照できる手段からは依然として読めます。以下ではその露出を抑える方法を示します。

### 機密変数

**機密値をバージョン管理にコミットしないでください:**

```bash
# ❌ 悪い例: ワークフローファイルにハードコード
vars:
  API_TOKEN: "secret-token-here"

# ✅ 良い例: 環境変数を参照
vars:
  api_token: "{{API_TOKEN}}"
```

### 変数管理

秘密情報は、残り続けるファイルに保存するより、使う時点でシークレットストアから取得するほうが安全です。

```bash
# セキュアな秘密管理を使用
export API_TOKEN=$(aws ssm get-parameter --name "/app/api-token" --with-decryption --query 'Parameter.Value' --output text)
export DB_PASSWORD=$(vault kv get -field=password secret/database)

# 複雑な秘密には一時ファイルを使用
echo "$GOOGLE_CREDENTIALS_JSON" > /tmp/gcp-key.json
export GOOGLE_APPLICATION_CREDENTIALS=/tmp/gcp-key.json
```

### 環境隔離

変数名に環境ごとの接頭辞を付けておけば、ステージングの実行が本番の認証情報を参照してしまうことを防げます。

```bash
# 競合を避けるために環境でプレフィックス変数
export PROD_API_TOKEN="prod-token"
export STAGING_API_TOKEN="staging-token"  
export DEV_API_TOKEN="dev-token"

# 環境固有の選択を使用
export API_TOKEN="${ENVIRONMENT}_API_TOKEN"
export API_TOKEN="${!API_TOKEN}"  # 間接変数展開
```

## 環境変数のデバッグ

テンプレートが空に解決されるときは、値が間違っているのではなく変数が存在しない場合がほとんどです。以下のコマンドでプロセスが実際に受け取った内容を確認します。

### 利用可能な変数の表示

ワークフローを調べる前に、シェルが実際にどの変数をエクスポートしているかを確認します。

```bash
# すべての環境変数をリスト
env | grep -E '^(PROBE_|API_|SMTP_)' | sort

# 特定の変数をチェック
echo "API_TOKEN: $API_TOKEN"
echo "PROBE_OUTPUT: $PROBE_OUTPUT"

# ワークフローでデバッグ（機密データに注意）
probe -v workflow.yml 2>&1 | grep -i "environment"
```

### テンプレートデバッグ

変数を出力するだけのワークフローを動かせば、テンプレートが何に解決されたかがわかります。

```yaml
# workflow.yml - 環境変数展開のデバッグ
vars:
  api_base_url: "{{API_BASE_URL}}"
  environment: "{{ENVIRONMENT}}"
  default_timeout: "{{DEFAULT_TIMEOUT ?? 'not set'}}"

steps:
  - name: "Debug Environment"
    uses: hello
    with:
      message: |
        Environment Debug:
        API_URL: "{{vars.api_base_url}}"
        Environment: "{{vars.environment}}"
        Timeout: "{{vars.default_timeout}}"
        
  - name: "Test Variable Access"
    uses: echo
    with:
      message: |
        Available variables:
        {{range $key, $value := vars}}
          {{$key}}: {{$value}}
        {{end}}
```

## 一般的なパターン

以下では既定値、環境変数、条件を組み合わせて、1つのワークフローを実行環境に合わせて振る舞わせる方法を示します。

### 設定カスケード

設定ファイルを層にして重ねれば、内容を複製せずに、より限定的な設定で一般的な設定を上書きできます。

```bash
# システムデフォルト
export SYSTEM_CONFIG=/etc/probe/system.yml

# チームデフォルト  
export TEAM_CONFIG=/opt/team/defaults.yml

# プロジェクト固有
export PROJECT_CONFIG=./probe-defaults.yml

# カスケードでランタイム実行
probe ${SYSTEM_CONFIG},${TEAM_CONFIG},${PROJECT_CONFIG},workflow.yml
```

### 動的設定

実行するファイルを環境変数から選び、環境固有のファイルがない場合は既定のファイルに戻します。

```bash
# 環境に基づいて設定を生成
WORKFLOW_FILE="workflow-${ENVIRONMENT}.yml"
if [[ ! -f "$WORKFLOW_FILE" ]]; then
  WORKFLOW_FILE="workflow.yml"
fi

probe "$WORKFLOW_FILE"
```

### 条件付き実行

フラグをシェル側で求めておけば、ワークフロー内の条件は単純な比較のままで済みます。

```bash
# 環境に基づいて特定のジョブをスキップ
export SKIP_PERFORMANCE_TESTS=$([[ "$ENVIRONMENT" == "development" ]] && echo "true" || echo "false")
export ENABLE_SLACK_ALERTS=$([[ "$ENVIRONMENT" == "production" ]] && echo "true" || echo "false")
```

```yaml
# workflow.yml
vars:
  skip_performance_tests: "{{SKIP_PERFORMANCE_TESTS}}"
  enable_slack_alerts: "{{ENABLE_SLACK_ALERTS}}"

jobs:
- name: performance-tests
  # パフォーマンステストステップ
    
- name: alerts
  needs: [performance-tests]
  # アラートステップ
```

## 関連項目

- **[CLIリファレンス](/ja/reference/cli-reference)** - コマンドライン環境変数の使用
- **[YAML設定](/ja/reference/yaml-configuration)** - ワークフローでの環境変数の使用
- **[ハウツー: 環境管理](/ja/guide/how-tos/environment-management)** - 実用的な環境管理戦略
- **[概念: ファイルマージ](/ja/guide/concepts/file-merging)** - 設定合成パターン
