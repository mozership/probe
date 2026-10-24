---
title: 最初のワークフロー
description: 実用的な例で実践的なワークフローの構築を学ぶ
weight: 40
---

# 最初のワークフロー

[核となる概念](/ja/guide/introduction/understanding-probe)を理解したので、実践的なワークフローをゼロから構築しましょう。このガイドでは、ウェブアプリケーション用の包括的な監視ワークフローの作成を説明します。

## シナリオ

完全なウェブアプリケーションスタックを監視するワークフローを作成します：

1. **フロントエンド**: ウェブアプリケーションが正しく読み込まれるかチェック
2. **API**: REST APIが応答しているか確認
3. **データベース**: API経由でデータベース接続をテスト
4. **外部サービス**: サードパーティサービス統合をチェック

## ステップ1: 基本構造

基本的なワークフロー構造から始めましょう：

```yaml
name: Web Application Health Check
description: Comprehensive monitoring for our web application stack

jobs:
  # ここにジョブを追加します
```

## ステップ2: フロントエンド監視

フロントエンドアプリケーションをチェックするジョブを追加：

```yaml
name: Web Application Health Check
description: Comprehensive monitoring for our web application stack

jobs:
- id: frontend-check
  name: Frontend Application Check
  steps:
    - name: Check Homepage
      id: frontend-check
      uses: http
      with:
        url: https://myapp.example.com
        method: GET
        headers:
          User-Agent: "Probe Health Check"
      test: res.code == 200 && (rt.sec * 1000) < 3000
      outputs:
        homepage_response_time: (rt.sec * 1000)

    - name: Check Critical Page
      uses: http
      with:
        url: https://myapp.example.com/dashboard
        method: GET
      test: res.code == 200 || res.code == 302

    - name: Report Frontend Status
      uses: hello
      echo: "✅ Frontend is healthy ({{outputs.homepage_response_time}}ms)"
```

## ステップ3: API監視

API監視用の別のジョブを追加：

```yaml
  api-check:
    name: API Health Check
    steps:
      - name: Check API Health Endpoint
        id: health-check
        uses: http
        with:
          url: https://api.myapp.example.com/health
          method: GET
          headers:
            Accept: "application/json"
        test: res.code == 200 && res.body.status == "healthy"
        outputs:
          api_version: res.body.version
          database_status: res.body.database

      - name: Test User Authentication
        uses: http
        with:
          url: https://api.myapp.example.com/auth/login
          method: POST
          headers:
            Content-Type: "application/json"
          body: |
            {
              "username": "healthcheck",
              "password": "{{vars.HEALTH_CHECK_PASSWORD}}"
            }
        test: res.code == 200 && res.body.token != nil
        outputs:
          auth_token: res.body.token

      - name: Test Authenticated Endpoint
        uses: http
        with:
          url: https://api.myapp.example.com/user/profile
          method: GET
          headers:
            Authorization: "Bearer {{outputs.auth_token}}"
        test: res.code == 200

      - name: Report API Status
        uses: hello
        echo: "✅ API v{{outputs.api_version}} is healthy"
```

## ステップ4: 依存関係の追加

APIチェックを成功したフロントエンドチェックに依存させる：

```yaml
  api-check:
    name: API Health Check
    needs: [frontend-check]  # フロントエンドが正常になるまで待機
    steps:
      # ... 既存のステップ
```

## ステップ5: 外部サービスチェック

外部サービス用のチェックを追加：

```yaml
  external-services:
    name: External Services Check
    steps:
      - name: Check Email Service
        uses: http
        with:
          url: https://api.sendgrid.com/v3/mail/send
          method: POST
          headers:
            Authorization: "Bearer {{vars.SENDGRID_API_KEY}}"
            Content-Type: "application/json"
          body: |
            {
              "from": {"email": "health@myapp.example.com"},
              "subject": "Health Check Test",
              "content": [{"type": "text/plain", "value": "Test"}],
              "personalizations": [{"to": [{"email": "test@myapp.example.com"}]}]
            }
        test: res.code == 202

      - name: Check Payment Gateway
        uses: http
        with:
          url: https://api.stripe.com/v1/charges
          method: GET
          headers:
            Authorization: "Bearer {{vars.STRIPE_SECRET_KEY}}"
        test: res.code == 200

      - name: Report External Services
        uses: hello
        echo: "✅ All external services are responding"
```

## ステップ6: エラーハンドリングと通知

エラーハンドリングと通知ロジックを追加：

```yaml
  notification:
    name: Send Notifications
    needs: [frontend-check, api-check, external-services]
    steps:
      - name: Success Notification
        uses: hello
        echo: |
          🎉 All systems are healthy!
          
          Frontend: ✅ ({{outputs['frontend-check'].homepage_response_time}}ms)
          API: ✅ v{{outputs['api-check'].api_version}}
          External Services: ✅
          
          Monitoring completed at {{unixtime()}}

      - name: Failure Notification
        uses: hello
        echo: |
          🚨 ALERT: System health check failed!
          
          
          Please investigate immediately.
```

## 完全なワークフロー

完全なワークフローファイル（`health-check.yml`）は以下の通りです：

```yaml
name: Web Application Health Check
description: Comprehensive monitoring for our web application stack

jobs:
- id: frontend-check
  name: Frontend Application Check
  steps:
    - name: Check Homepage
      id: frontend-check
      uses: http
      with:
        url: https://myapp.example.com
        method: GET
        headers:
          User-Agent: "Probe Health Check"
      test: res.code == 200 && (rt.sec * 1000) < 3000
      outputs:
        homepage_response_time: (rt.sec * 1000)

    - name: Check Critical Page
      uses: http
      with:
        url: https://myapp.example.com/dashboard
        method: GET
      test: res.code == 200 || res.code == 302

    - name: Report Frontend Status
      uses: hello
      echo: "✅ Frontend is healthy ({{outputs.homepage_response_time}}ms)"

- id: api-check
  name: API Health Check
  needs: [frontend-check]
  steps:
    - name: Check API Health Endpoint
      id: health-check
      uses: http
      with:
        url: https://api.myapp.example.com/health
        method: GET
        headers:
          Accept: "application/json"
      test: res.code == 200 && res.body.status == "healthy"
      outputs:
        api_version: res.body.version
        database_status: res.body.database

    - name: Test User Authentication
      id: api-check
      uses: http
      with:
        url: https://api.myapp.example.com/auth/login
        method: POST
        headers:
          Content-Type: "application/json"
        body: |
          {
            "username": "healthcheck",
            "password": "{{vars.HEALTH_CHECK_PASSWORD}}"
          }
      test: res.code == 200 && res.body.token != nil
      outputs:
        auth_token: res.body.token

    - name: Test Authenticated Endpoint
      uses: http
      with:
        url: https://api.myapp.example.com/user/profile
        method: GET
        headers:
          Authorization: "Bearer {{outputs.auth_token}}"
      test: res.code == 200

    - name: Report API Status
      uses: hello
      echo: "✅ API v{{outputs.api_version}} is healthy"

- id: external-services
  name: External Services Check
  steps:
    - name: Check Email Service
      uses: http
      with:
        url: https://api.sendgrid.com/v3/mail/send
        method: POST
        headers:
          Authorization: "Bearer {{vars.SENDGRID_API_KEY}}"
          Content-Type: "application/json"
        body: |
          {
            "from": {"email": "health@myapp.example.com"},
            "subject": "Health Check Test",
            "content": [{"type": "text/plain", "value": "Test"}],
            "personalizations": [{"to": [{"email": "test@myapp.example.com"}]}]
          }
      test: res.code == 202

    - name: Check Payment Gateway
      uses: http
      with:
        url: https://api.stripe.com/v1/charges
        method: GET
        headers:
          Authorization: "Bearer {{vars.STRIPE_SECRET_KEY}}"
      test: res.code == 200

    - name: Report External Services
      uses: hello
      echo: "✅ All external services are responding"

- id: notification
  name: Send Notifications
  needs: [frontend-check, api-check, external-services]
  steps:
    - name: Success Notification
      uses: hello
      echo: |
        🎉 All systems are healthy!
          
        Frontend: ✅ ({{outputs['frontend-check'].homepage_response_time}}ms)
        API: ✅ v{{outputs['api-check'].api_version}}
        External Services: ✅
          
        Monitoring completed at {{unixtime()}}

    - name: Failure Notification
      uses: hello
      echo: |
        🚨 ALERT: System health check failed!
          
          
        Please investigate immediately.
```

## ワークフローの実行

このワークフローは値を環境変数から読むため、まずそれらを設定します。確認したいことがあれば、詳細を出力して実行し直せます。

### 環境変数の設定

まず、環境変数を設定します：

```bash
export HEALTH_CHECK_PASSWORD="your-test-password"
export SENDGRID_API_KEY="your-sendgrid-key"
export STRIPE_SECRET_KEY="your-stripe-key"
```

### ワークフローの実行

ワークフローを実行：

```bash
probe health-check.yml
```

### デバッグ用の詳細モードを使用

開発中の詳細出力には：

```bash
probe -v health-check.yml
```

## 本番環境対応にする

動かし続けられるワークフローにするには3つ足りません。環境ごとの設定、一時的な失敗を許容する仕組み、そして実行スケジュールです。

### 1. 環境固有の設定

環境固有の設定ファイルを作成：

**production.yml:**
```yaml
# 本番環境用のURLをオーバーライド
variables:
  frontend_url: https://app.mycompany.com
  api_url: https://api.mycompany.com
```

**staging.yml:**
```yaml
# ステージング環境用のURLをオーバーライド
variables:
  frontend_url: https://staging.mycompany.com
  api_url: https://api-staging.mycompany.com
```

環境固有の設定で実行：
```bash
probe health-check.yml,production.yml
```

### 2. リトライロジックの追加

無人で動く確認では、一時的な失敗を1回は許容できるようにしておきます。

```yaml
- name: Check Critical Service
  uses: http
  with:
    url: https://critical-service.example.com
    method: GET
    retry_count: 3
    retry_delay: 5s
  test: res.code == 200
```

### 3. 監視スケジュールの設定

定期的に実行するためにcronを使用：
```bash
# crontabに追加 - 5分ごとに実行
*/5 * * * * /usr/local/bin/probe /path/to/health-check.yml
```

## 学んだこと

このガイドでは、以下の方法を学びました：

- ✅ マルチジョブワークフローの構造化
- ✅ `needs`でジョブ依存関係を使用
- ✅ `outputs`を使用してステップ間でデータを渡す
- ✅ API呼び出しで認証を処理
- `skipif`で条件ロジックを実装
- ✅ 設定のために環境変数を使用
- ✅ 包括的なエラーハンドリングを作成
- ✅ 異なる環境用に設定ファイルをマージ

## 次のステップ

さらに深く学ぶ準備はできましたか？次のステップは：

1. **[CLIをマスター](/ja/guide/introduction/cli-basics)** - すべてのコマンドライン・オプションを学ぶ
2. **[How-tosを探る](/ja/guide/how-tos/api-testing)** - 具体的な使用例とパターンを見る
3. **[リファレンスを参照](/ja/reference/actions/variables)** - 利用可能なすべての機能を深く理解

構築したワークフローには、さらに手を加えられます。より多くのチェックを追加したり、監視システムと統合したり、特定のアプリケーションスタック用にカスタマイズしたりして拡張できます。
