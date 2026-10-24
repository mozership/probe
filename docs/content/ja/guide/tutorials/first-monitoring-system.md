# 初めての監視システム構築

このチュートリアルでは、Probeを使用してWebアプリケーションのヘルス、パフォーマンス、可用性を監視する完全な監視システムを構築します。最終的に、複数のエンドポイントをチェックし、レスポンスを検証し、問題が検出されたときにアラートを送信するプロダクション対応の監視ワークフローが完成します。

## 構築する内容

以下の機能を持つ包括的な監視システム：

- **ヘルスチェック** - 複数のAPIエンドポイント
- **パフォーマンス監視** - レスポンス時間の追跡
- **データ検証** - APIレスポンスが正しいことを確認
- **エラーハンドリング** - 自動リトライとフォールバックチェック
- **アラート通知** - 問題検出時のメール通知
- **環境設定** - 開発、ステージング、プロダクション用

## 前提条件

- Probeがインストール済み（[インストールガイド](/ja/guide/introduction/installation)）
- HTTP APIの基本的な理解
- 通知用のSMTPサーバー。Probeを実行するマシンからのメールを、ログインなしで受け付けるもの（社内のメールリレーなど）
- 監視対象のWebアプリケーションまたはAPI（なければ例を提供します）

## ステップ1: 監視戦略の計画

コードを書く前に、何を監視するかを計画しましょう：

### ターゲットアプリケーション

このチュートリアルでは、以下のエンドポイントを持つ架空のeコマースAPIを監視します：

- `GET /health` - 基本ヘルスチェック
- `GET /api/products` - 商品カタログの可用性  
- `GET /api/users/me` - ユーザー認証システム
- `GET /api/orders/recent` - 注文処理システム
- `POST /api/search` - 検索機能

### 成功基準

監視では以下を検証します：

- **可用性**: すべてのエンドポイントが成功レスポンスを返すこと
- **パフォーマンス**: レスポンス時間が許容範囲内であること
- **データ整合性**: レスポンスに期待されるデータ構造が含まれること
- **認証**: 保護されたエンドポイントが適切な認証を要求すること

## ステップ2: 基本監視ワークフローの作成

シンプルな監視ワークフローから始めましょう：

```yaml
# monitoring.yml
name: "E-commerce API Monitoring"
description: |
  ヘルスチェック、パフォーマンス監視、データ検証を含む
  eコマースAPIエンドポイントの包括的監視。

vars:
  # API設定
  API_BASE_URL: "https://api.example.com"
  API_VERSION: "v1"
  
  # パフォーマンスしきい値
  MAX_RESPONSE_TIME: 2000  # 2秒
  CRITICAL_RESPONSE_TIME: 5000  # 5秒
  
  # 認証
  API_TOKEN: "{{API_AUTH_TOKEN}}"

jobs:
- id: health-check
  name: "Basic Health Check"
  defaults:
    http:
      headers:
        User-Agent: "Probe Monitor v1.0"
        Accept: "application/json"
  steps:
    - name: "Check API Health Endpoint"
      id: health
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.MAX_RESPONSE_TIME
      outputs:
        status: res.body.status
        response_time: (rt.sec * 1000)
        healthy: res.code == 200 && res.body.status == "healthy"
```

**これを`monitoring.yml`として保存してテストします：**

```bash
# APIトークンを設定
export API_AUTH_TOKEN="your-api-token-here"

# 基本監視を実行
probe monitoring.yml
```

## ステップ3: 包括的エンドポイント監視の追加

すべての重要なエンドポイントを監視するように拡張しましょう：

```yaml
# monitoring.ymlのjobsリストで、health-checkジョブの後にこのジョブを追加

- id: endpoint-monitoring
  name: "API Endpoint Monitoring"
  needs: [health-check]
  steps:
    - name: "Test Product Catalog"
      id: products
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.MAX_RESPONSE_TIME &&
        res.body.products != nil &&
        len(res.body.products) > 0
      outputs:
        product_count: len(res.body.products)
        response_time: (rt.sec * 1000)
        available: res.code == 200

    - name: "Test User Authentication"
      id: auth
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/users/me"
        headers:
          Authorization: "Bearer {{vars.API_TOKEN}}"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.MAX_RESPONSE_TIME &&
        res.body.user != nil &&
        res.body.user.id != nil
      outputs:
        authenticated: res.code == 200
        user_id: res.body.user.id
        response_time: (rt.sec * 1000)

    - name: "Test Order System"
      id: orders
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/orders/recent"
        headers:
          Authorization: "Bearer {{vars.API_TOKEN}}"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.MAX_RESPONSE_TIME &&
        res.body.orders != nil
      outputs:
        order_count: len(res.body.orders)
        response_time: (rt.sec * 1000)
        available: res.code == 200

    - name: "Test Search Functionality"
      id: search
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/search"
        method: "POST"
        headers:
          Authorization: "Bearer {{vars.API_TOKEN}}"
          Content-Type: "application/json"
        body: |
          {
            "query": "test product",
            "limit": 10
          }
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.MAX_RESPONSE_TIME &&
        res.body.results != nil
      outputs:
        result_count: len(res.body.results)
        response_time: (rt.sec * 1000)
        available: res.code == 200
```

## ステップ4: パフォーマンス分析の追加

全体的なパフォーマンスを分析するジョブを追加しましょう：

```yaml
# monitoring.ymlのjobsリストにこのジョブを追加

- id: performance-analysis
  name: "Performance Analysis"
  needs: [health-check, endpoint-monitoring]
  always: true  # チェックが失敗したときもレポートする
  steps:
    - name: "Calculate Performance Metrics"
      id: metrics
      uses: hello
      with:
        # 応答したエンドポイントの平均レスポンス時間
        avg_response_time: >-
          {{ mean(filter([
            outputs.health?.response_time,
            outputs.products?.response_time,
            outputs.auth?.response_time,
            outputs.orders?.response_time,
            outputs.search?.response_time
          ], # != nil)) }}
        # 成功したエンドポイント数をカウント
        successful_endpoints: >-
          {{ count([
            outputs.health?.healthy,
            outputs.products?.available,
            outputs.auth?.authenticated,
            outputs.orders?.available,
            outputs.search?.available
          ], # == true) }}
      outputs:
        avg_response_time: res.avg_response_time
        successful_endpoints: res.successful_endpoints
        # 成功率パーセンテージを計算
        success_rate: res.successful_endpoints * 100 / 5
        # 全体的なヘルス状態を決定
        overall_status: |
          res.successful_endpoints == 5 ? "healthy" :
          res.successful_endpoints >= 4 ? "degraded" : "critical"

    - name: "Performance Report"
      uses: hello
      echo: |
        === MONITORING REPORT ===

        Overall Status: {{upper(outputs.metrics.overall_status)}}
        Success Rate: {{outputs.metrics.success_rate}}%
        Average Response Time: {{outputs.metrics.avg_response_time}}ms

        Endpoint Details:
        ✓ Health Check: {{outputs.health?.healthy == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.health?.response_time ?? "-"}}ms)
        ✓ Products API: {{outputs.products?.available == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.products?.response_time ?? "-"}}ms)
        ✓ Authentication: {{outputs.auth?.authenticated == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.auth?.response_time ?? "-"}}ms)
        ✓ Orders API: {{outputs.orders?.available == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.orders?.response_time ?? "-"}}ms)
        ✓ Search API: {{outputs.search?.available == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.search?.response_time ?? "-"}}ms)

        Data Summary:
        - Products Available: {{outputs.products?.product_count ?? 0}}
        - Recent Orders: {{outputs.orders?.order_count ?? 0}}
        - Search Results: {{outputs.search?.result_count ?? 0}}

        Generated: {{now().Format('2006-01-02T15:04:05Z07:00')}}
```

`always: true`を付けると、依存するジョブが失敗したときもこのジョブが実行されます。レポートが必要になるのは、まさにそのときです。失敗したジョブやスキップされたジョブが公開しなかった出力はnilになるため、このジョブは各出力を`?.`で読みます。1つのステップの`outputs`どうしは互いを読めないため、件数は`with`で計算します。`hello`は`with`に渡された値を`res`で返すので、`outputs`はその値から組み立てます。

## ステップ5: エラーハンドリングとリトライの追加

エラーハンドリングで監視をより堅牢にしましょう：

```yaml
# endpoint-monitoringジョブをこの拡張版で置き換える

- id: endpoint-monitoring
  name: "API Endpoint Monitoring"
  needs: [health-check]
  steps:
    - name: "Test Product Catalog"
      id: products
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.MAX_RESPONSE_TIME &&
        res.body.products != nil &&
        len(res.body.products) > 0
      retry:
        max_attempts: 3  # 失敗とする前に最大3回試す
        interval: 2s
      outputs:
        product_count: len(res.body.products ?? [])
        response_time: (rt.sec * 1000)
        available: res.code == 200
        status_code: res.code

    - name: "Test User Authentication"
      id: auth
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/users/me"
        headers:
          Authorization: "Bearer {{vars.API_TOKEN}}"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.MAX_RESPONSE_TIME &&
        res.body.user != nil &&
        res.body.user.id != nil
      retry:
        max_attempts: 3
        interval: 2s
      outputs:
        authenticated: res.code == 200
        user_id: res.body.user?.id ?? "unknown"
        response_time: (rt.sec * 1000)
        status_code: res.code

    # ordersとsearchのステップも同様のパターンで続ける...
```

`retry`はテストが通るまでステップを再実行するため、1回だけ遅れたり失われたりした応答ではチェックは失敗しません。`??`はレスポンスにフィールドがないときの値を与えるため、APIがエラーを返したときも出力が公開されます。

## ステップ6: メール通知の追加

問題が検出されたときのメールアラートを追加しましょう。アラートのジョブはメールの設定を環境変数から読むため、`monitoring.yml`の`vars`に以下を追加します：

```yaml
vars:
  # メール設定
  SMTP_HOST: "{{SMTP_HOST ?? 'localhost'}}"
  SMTP_PORT: "{{SMTP_PORT ?? '25'}}"
  ALERT_FROM_EMAIL: "{{ALERT_FROM_EMAIL}}"
  ALERT_TO_EMAIL: "{{ALERT_TO_EMAIL}}"
```

続いてジョブを追加します：

```yaml
# 監視で問題が検出されたときにアラートを送信するジョブを追加

- id: alerting
  name: "Alert Notifications"
  needs: [performance-analysis]
  skipif: outputs.metrics.overall_status == "healthy"
  steps:
    - name: "Send Alert Email"
      uses: smtp
      with:
        addr: "{{vars.SMTP_HOST}}:{{vars.SMTP_PORT}}"
        from: "{{vars.ALERT_FROM_EMAIL}}"
        to: "{{vars.ALERT_TO_EMAIL}}"
        subject: "🚨 API Monitoring Alert - {{upper(outputs.metrics.overall_status)}}"
        session: 1
        message: 1
        length: 500
      test: res.code == 0 && res.sent > 0
      echo: |
        API MONITORING ALERT
        ====================

        Alert Summary:
        Overall Status: {{upper(outputs.metrics.overall_status)}}
        Success Rate: {{outputs.metrics.success_rate}}%
        Average Response Time: {{outputs.metrics.avg_response_time}}ms
        Time: {{now().Format('2006-01-02T15:04:05Z07:00')}}

        Endpoint Status:
        Health Check: {{outputs.health?.healthy == true ? "✅ UP" : "❌ DOWN"}} (status: {{outputs.health?.status ?? "-"}})
        Products API: {{outputs.products?.available == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.products?.product_count ?? 0}} products)
        Authentication: {{outputs.auth?.authenticated == true ? "✅ UP" : "❌ DOWN"}} (user ID: {{outputs.auth?.user_id ?? "-"}})
        Orders API: {{outputs.orders?.available == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.orders?.order_count ?? 0}} recent orders)
        Search API: {{outputs.search?.available == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.search?.result_count ?? 0}} search results)

        Recommended Actions:
        {{outputs.health?.healthy != true ? "- Check API server health and connectivity" : ""}}
        {{outputs.products?.available != true ? "- Investigate product catalog service" : ""}}
        {{outputs.auth?.authenticated != true ? "- Verify authentication service and token validity" : ""}}
        {{outputs.orders?.available != true ? "- Check order processing system" : ""}}
        {{outputs.search?.available != true ? "- Investigate search service functionality" : ""}}
        {{outputs.metrics.avg_response_time > 3000 ? "- Performance issue detected - investigate slow responses" : ""}}

        This alert was generated by Probe Monitoring System.

    - name: "Log Alert Sent"
      uses: hello
      echo: |
        🚨 ALERT SENT
        Status: {{outputs.metrics.overall_status}}
        Success Rate: {{outputs.metrics.success_rate}}%
        Alert sent to: {{vars.ALERT_TO_EMAIL}}
```

すべてのエンドポイントが正常な間は、`skipif`によってこのジョブはスキップされます。`smtp`アクションは、上の件名を付けて自動生成のメッセージを送ります。本文を指定するパラメータも、サーバーにログインするためのパラメータもないため、このホストからのメールを認証なしで受け付けるサーバー（社内のメールリレーなど）を使ってください。レポートはステップの`echo`に書き、Probeの出力に表示されます。

## ステップ7: 環境設定

環境固有の設定ファイルを作成します。Probeは渡されたファイルを1つのドキュメントにつなげるため、後のファイルの`vars`は`monitoring.yml`の`vars`を項目ごとではなく丸ごと置き換えます。そのため、各ファイルには環境変数を読むものも含めてすべての変数を書きます：

**development.yml:**
```yaml
vars:
  API_BASE_URL: "http://localhost:3000"
  API_VERSION: "v1"
  MAX_RESPONSE_TIME: 5000  # 開発環境では緩い上限
  CRITICAL_RESPONSE_TIME: 10000
  API_TOKEN: "{{API_AUTH_TOKEN}}"
  SMTP_HOST: "{{SMTP_HOST ?? 'localhost'}}"
  SMTP_PORT: "{{SMTP_PORT ?? '25'}}"
  ALERT_FROM_EMAIL: "{{ALERT_FROM_EMAIL}}"
  ALERT_TO_EMAIL: "{{ALERT_TO_EMAIL}}"
```

**staging.yml:**
```yaml
vars:
  API_BASE_URL: "https://api-staging.example.com"
  API_VERSION: "v1"
  MAX_RESPONSE_TIME: 3000
  CRITICAL_RESPONSE_TIME: 7000
  API_TOKEN: "{{API_AUTH_TOKEN}}"
  SMTP_HOST: "{{SMTP_HOST ?? 'localhost'}}"
  SMTP_PORT: "{{SMTP_PORT ?? '25'}}"
  ALERT_FROM_EMAIL: "{{ALERT_FROM_EMAIL}}"
  ALERT_TO_EMAIL: "{{ALERT_TO_EMAIL}}"
```

**production.yml:**
```yaml
vars:
  API_BASE_URL: "https://api.example.com"
  API_VERSION: "v1"
  MAX_RESPONSE_TIME: 2000
  CRITICAL_RESPONSE_TIME: 5000
  API_TOKEN: "{{API_AUTH_TOKEN}}"
  SMTP_HOST: "{{SMTP_HOST ?? 'localhost'}}"
  SMTP_PORT: "{{SMTP_PORT ?? '25'}}"
  ALERT_FROM_EMAIL: "{{ALERT_FROM_EMAIL}}"
  ALERT_TO_EMAIL: "{{ALERT_TO_EMAIL}}"
```

## ステップ8: 環境変数の設定

環境変数を設定するスクリプトを作成します：

**setup-monitoring.sh:**
```bash
#!/bin/bash

# API設定
export API_AUTH_TOKEN="your-api-token-here"

# メール設定
export SMTP_HOST="mail.yourcompany.com"
export SMTP_PORT=25
export ALERT_FROM_EMAIL="alerts@yourcompany.com"
export ALERT_TO_EMAIL="admin@yourcompany.com"

# 環境選択
export ENVIRONMENT=${1:-development}

echo "Environment configured for: $ENVIRONMENT"
echo "API Token: ${API_AUTH_TOKEN:0:10}..."
echo "Alert Email: $ALERT_TO_EMAIL"
```

## ステップ9: 監視システムの実行

完全な監視システムを実行できます：

```bash
# 環境を設定
chmod +x setup-monitoring.sh
source setup-monitoring.sh production

# 特定の環境での監視実行
probe monitoring.yml,production.yml

# 詳細出力で実行
probe -v monitoring.yml,staging.yml

# 開発環境での実行
source setup-monitoring.sh development
probe monitoring.yml,development.yml
```

## ステップ10: Cronで自動化

cronで自動監視を設定します：

```bash
# crontabを編集
crontab -e

# 定期監視のエントリを追加
# setup-monitoring.shは環境変数をexportするため、bashでsourceする
SHELL=/bin/bash

# 営業時間中は5分毎に実行
*/5 9-17 * * 1-5 source /path/to/setup-monitoring.sh production && probe /path/to/monitoring.yml,/path/to/production.yml >> /var/log/probe-monitoring.log 2>&1

# 営業時間外は15分毎に実行
*/15 0-8,18-23 * * * source /path/to/setup-monitoring.sh production && probe /path/to/monitoring.yml,/path/to/production.yml >> /var/log/probe-monitoring.log 2>&1

# 週末は1時間毎に実行
0 * * * 0,6 source /path/to/setup-monitoring.sh production && probe /path/to/monitoring.yml,/path/to/production.yml >> /var/log/probe-monitoring.log 2>&1
```

## テストと検証

監視を定期実行に切り替える前に、各部分が動くことを確認します。個々のジョブと、障害時に届くはずのアラートです。

### 個別コンポーネントのテスト

ジョブを1つずつ実行して各部分を確認してから、ワークフロー全体を無人で動かします。

```bash
# ヘルスチェックをテスト（alwaysジョブのパフォーマンスレポートも後に実行される）
probe -v --job health-check monitoring.yml

# メールアラートなしでテスト（分析に必要なジョブを実行し、アラートは実行しない）
probe --job performance-analysis monitoring.yml,production.yml

# 偽の失敗でテスト（存在しないエンドポイントにURLを変更）
probe monitoring.yml,development.yml
```

### メールアラートの検証

1. 何も応答しないアドレスにチェックを向けて、すべてのチェックを失敗させる：`probe --var API_BASE_URL=http://localhost:1 monitoring.yml`
2. メールが配信されたことを確認
3. メールの件名と、Probeの出力のレポートをチェック
4. `--var`なしで再度実行し、アラートのジョブがスキップされることを確認

## トラブルシューティング

監視システムがここまでの説明どおりに動かない場合は、以下を順に確認してください。

### よくある問題

**認証失敗:**
```bash
# APIトークンをチェック
curl -H "Authorization: Bearer $API_AUTH_TOKEN" https://api.example.com/api/v1/users/me

# トークンの有効期限を確認
echo $API_AUTH_TOKEN | base64 -d  # JWTトークンの場合
```

**メール配信の問題:**
```bash
# SMTP接続をテスト
telnet "$SMTP_HOST" "$SMTP_PORT"

# Probeはサーバーにログインしないため、サーバーはこのホストからの
# メールを認証なしで受け付ける必要がある
```

**パフォーマンスの問題:**
```bash
# ネットワーク接続をチェック
ping api.example.com

# レスポンス時間を手動でテスト
curl -w "@curl-format.txt" -o /dev/null -s https://api.example.com/health
```

**JSON解析エラー:**
```bash
# APIレスポンスを手動で検証
curl -H "Authorization: Bearer $API_AUTH_TOKEN" https://api.example.com/api/v1/products | jq .
```

## 次のステップ

おめでとうございます！包括的な監視システムを構築しました。拡張方法：

1. **データベース監視の追加** - データベース接続とパフォーマンスの監視
2. **ダッシュボード統合** - Grafanaなどのツールにメトリクスを送信
3. **アラートチャネルの追加** - Slack、PagerDuty、またはwebhook通知
4. **スマートアラートの実装** - 連続した複数の失敗後のみアラート
5. **ビジネスロジックテストの追加** - 単なるエンドポイントではなく重要なユーザージャーニーのテスト
6. **パフォーマンス回帰検出** - 時間の経過に伴うレスポンス時間の比較
7. **SSL証明書監視** - 証明書有効期限日のチェック

## 関連リソース

- **[APIテストパイプラインチュートリアル](/ja/guide/tutorials/api-testing-pipeline)** - 高度なAPIテストパターン
- **[マルチ環境テストチュートリアル](/ja/guide/tutorials/multi-environment-testing)** - 複数環境でのテスト
- **[ハウツー: パフォーマンステスト](/ja/guide/how-tos/performance-testing)** - 応答時間への上限と、専用のツールによる負荷試験
- **[ハウツー: モニタリングワークフロー](/ja/guide/how-tos/monitoring-workflows)** - 追加の監視パターン
