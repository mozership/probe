# データフロー

データフローは、情報がProbeワークフローを通して移動するメカニズムです。データフローパターンを理解することで、ステップ、ジョブ、さらには外部システム間で情報をやり取りする高度なワークフローを構築できます。このガイドではProbeの完全なデータフローシステムについて詳しく説明します。

## データフロー概要

Probeは構造化されたデータフローアプローチを使用します：

1. **入力ソース**: 環境変数、設定ファイル、ユーザー入力
2. **処理**: アクションがレスポンスと出力を生成
3. **ストレージ**: 出力が後で使用するために保存
4. **伝播**: データがステップとジョブ間を流れる
5. **消費**: 他のステップが動的設定のためにデータを使用

## データソース

ワークフローは扱う値を外部から受け取ります。環境変数と、読み込んだ設定ファイルです。

### 環境変数

環境変数は外部設定と実行時コンテキストを提供します。

```yaml
# 環境変数にアクセス
steps:
  - name: Environment-based Configuration
    uses: http
    with:
      method: GET
      url: "{{vars.API_BASE_URL}}/{{vars.API_VERSION}}/users"
      headers:
        Authorization: "Bearer {{vars.API_TOKEN}}"
        X-Environment: "{{vars.DEPLOYMENT_ENV}}"
    test: res.code == 200
```

### 設定マージ

データはマージされた設定ファイルから取得できます：

**vars.yml:**
```yaml
vars:
  api_base_url: "{{API_BASE_URL ?? 'https://api.example.com'}}"
  api_token: "{{API_TOKEN}}"
```

**production.yml:**
```yaml
vars:
  api_base_url: https://api.production.example.com
  api_token: "{{PROD_API_TOKEN}}"
```

使用方法:
```bash
probe workflow.yml,production.yml
```

複数のファイルで定義されたトップレベルのキーは最後のファイルの値になり、キーごと置き換わります。詳しくは[ファイルマージ](/ja/guide/concepts/file-merging)を参照してください。

## ステップ出力

ステップは後続のステップやジョブで消費可能な出力を生成します。

### 基本的な出力定義

`outputs`では、ステップが公開する値の名前と、それを求める式を指定します。

```yaml
steps:
  - name: User Authentication
    id: auth
    uses: http
    with:
      url: "{{vars.API_URL}}/auth/login"
      method: POST
      body: |
        {
          "username": "{{vars.USERNAME}}",
          "password": "{{vars.PASSWORD}}"
        }
    test: res.code == 200
    outputs:
      access_token: res.body.access_token
      refresh_token: res.body.refresh_token
      user_id: res.body.user.id
      expires_at: res.body.expires_at
      user_roles: res.body.user.roles
```

### 出力データタイプ

出力にはさまざまなデータタイプを含むことができます：

```yaml
- name: Comprehensive Data Collection
  id: data-collection
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/comprehensive-data"
  test: res.code == 200
  outputs:
    # シンプルな値
    user_count: res.body.stats.user_count
    server_version: res.body.version
    is_healthy: res.body.health.status == "healthy"

    # 複雑なオブジェクト
    user_profile: res.body.user
    configuration: res.body.config
    metrics: res.body.metrics

    # 配列
    active_users: 'filter(res.body.users, #.active == true)'
    error_codes: 'map(res.body.errors, #.code)'

    # 計算された値
    success_rate: (res.body.successful_requests / res.body.total_requests) * 100
    avg_response_time: sum(res.body.response_times) / len(res.body.response_times)

    # レスポンスメタデータ
    response_time: (rt.sec * 1000)
    response_size: len(res.rawbody ?? res.body)
    content_type: res.headers["Content-Type"]
```

### 出力スコープ

出力は含まれるステップにスコープされ、IDで参照できます：

```yaml
steps:
  - name: Database Setup
    id: db-setup
    uses: http
    with:
      method: GET
      url: "{{vars.DB_API}}/initialize"
    outputs:
      db_session_id: res.body.session_id
      db_host: res.body.host
      db_port: res.body.port

  - name: Application Test
    id: app-test
    uses: http
    with:
      method: GET
      url: "{{vars.APP_URL}}/test"
      headers:
        X-DB-Session: "{{outputs['db-setup'].db_session_id}}"
        X-DB-Host: "{{outputs['db-setup'].db_host}}"
    outputs:
      test_result: res.body.result
      test_duration: (rt.sec * 1000)

  - name: Performance Analysis
    uses: hello
    echo: |
      Performance Analysis:
      Database: {{outputs['db-setup'].db_host}}:{{outputs['db-setup'].db_port}}
      Test Result: {{outputs['app-test'].test_result}}
      Test Duration: {{outputs['app-test'].test_duration}}ms
```

出力は`outputs.test_result`のように名前だけでも読めます。複数のステップが同じ名前を公開した場合、その名前は最初に公開したステップのものになります。`outputs.<name>`はそのステップの値を保ち、Probeは後から公開したステップごとに警告を出します。後のステップの値は、そのステップのidから読みます。ジョブ内のステップは順に実行されるため、どのステップが先かは決まっています。一方、同時に動くジョブの間では決まらないため、そうしたジョブが共有する名前はステップのidから読んでください。ステップのidと同じ名前は、そのステップの出力を指したままとなり、その名前を公開したステップにも同じように警告が出ます。

## ジョブ間データフロー

データはジョブレベルの出力と依存関係を通してジョブ間を流れることができます。

### ジョブ依存関係とデータ共有

`needs`はジョブの順序を決めると同時に、先行するジョブの出力を参照できるようにします。

```yaml
vars:
  SETUP_API: "{{SETUP_API}}"

jobs:
- id: initialization
  name: System Initialization
  steps:
    - name: Create Test Environment
      id: env-setup
      uses: http
      with:
        url: "{{vars.SETUP_API}}/create-environment"
        method: POST
        body: |
          {
            "environment_name": "test_{{random_str(8)}}",
            "configuration": "standard"
          }
      test: res.code == 201
      outputs:
        environment_id: res.body.environment.id
        environment_name: res.body.environment.name
        database_url: res.body.environment.database_url
        api_endpoint: res.body.environment.api_endpoint

- id: api-tests
  name: API Testing Suite
  needs: [initialization]  # initialization の完了を待つ
  steps:
    - name: Test User API
      id: user-api
      uses: http
      with:
        method: GET
        url: "{{outputs['env-setup'].api_endpoint}}/users"
        headers:
          X-Environment: "{{outputs['env-setup'].environment_id}}"
      test: res.code == 200
      outputs:
        user_count: res.body.total_users
        api_response_time: (rt.sec * 1000)

    - name: Test Database Connectivity
      id: db-check
      uses: http
      with:
        method: GET
        url: "{{outputs['env-setup'].database_url}}/ping"
      test: res.code == 200
      outputs:
        db_response_time: (rt.sec * 1000)

- id: reporting
  name: Test Reporting
  needs: [initialization, api-tests]  # 両ジョブを待つ
  steps:
    - name: Generate Test Report
      uses: hello
      echo: |
        Test Execution Report
        =====================

        Environment: {{outputs['env-setup'].environment_name}}
        Environment ID: {{outputs['env-setup'].environment_id}}

        API Tests:
        - User Count: {{outputs['user-api'].user_count}}
        - API Response Time: {{outputs['user-api'].api_response_time}}ms

        Database Tests:
        - DB Response Time: {{outputs['db-check'].db_response_time}}ms

        Overall Status: All tests completed successfully

- id: cleanup
  name: Environment Cleanup
  needs: [reporting]  # reporting 完了後に実行
  steps:
    - name: Destroy Test Environment
      uses: http
      with:
        url: "{{vars.SETUP_API}}/environments/{{outputs['env-setup'].environment_id}}"
        method: DELETE
      test: res.code == 204
```

### ジョブ間出力参照

他のジョブの出力は、ジョブのidではなく、公開したステップのidから`outputs['step-id'].output_name`のように読みます。この例ではジョブとステップが同じid`data-collection`を持っています：

```yaml
vars:
  API_URL: "{{API_URL}}"

jobs:
- id: data-collection
  name: data-collection
  steps:
    - name: Collect User Data
      id: data-collection
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users/stats"
      test: res.code == 200
      outputs:
        total_users: res.body.count
        active_users: res.body.active_count

- id: analysis
  name: analysis
  needs: [data-collection]
  steps:
    - name: Analyze User Metrics
      uses: hello
      echo: |
        User Analysis:
        Total Users: {{outputs['data-collection'].total_users}}
        Active Users: {{outputs['data-collection'].active_users}}
        Activity Rate: {{(outputs['data-collection'].active_users / outputs['data-collection'].total_users) * 100}}%
```

## 高度なデータフローパターン

データは必ずしもステップからステップへそのまま渡されるわけではありません。途中で変換したり、条件で経路を分けたり、複数のステップにまたがって蓄積したりできます。

### データ変換チェーン

複数のステップを通してデータを変換します：

```yaml
vars:
  DATA_API: "{{DATA_API}}"

jobs:
- id: data-processing-pipeline
  name: Data Processing Pipeline
  steps:
    - name: Fetch Raw Data
      id: raw-data
      uses: http
      with:
        method: GET
        url: "{{vars.DATA_API}}/raw-data"
      test: res.code == 200
      outputs:
        raw_records: res.body.records
        total_count: res.body.total
        fetch_time: (rt.sec * 1000)

    - name: Filter Data
      uses: hello
      id: filtered-data
      echo: "Filtering data..."
      outputs:
        # アクティブなレコードをフィルタ
        active_records: "filter(outputs['raw-data'].raw_records, #.status == 'active')"
        active_count: "len(filter(outputs['raw-data'].raw_records, #.status == 'active'))"

    - name: Aggregate Data
      uses: hello
      id: aggregated-data
      echo: "Aggregating data..."
      outputs:
        # カテゴリでグループ化してメトリクスを計算
        categories: "groupBy(outputs['filtered-data'].active_records, #.category)"
        avg_score: "sum(map(outputs['filtered-data'].active_records, #.score)) / outputs['filtered-data'].active_count"

    - name: Generate Summary
      uses: hello
      echo: |
        Data Processing Summary:

        Raw Records: {{outputs['raw-data'].total_count}}
        Active Records: {{outputs['filtered-data'].active_count}}
        Processing Rate: {{(outputs['filtered-data'].active_count / outputs['raw-data'].total_count) * 100}}%
        Average Score: {{outputs['aggregated-data'].avg_score}}
        Fetch Time: {{outputs['raw-data'].fetch_time}}ms
```

### 条件付きデータフロー

条件に基づいてデータフローを制御します：

```yaml
vars:
  API_URL: "{{API_URL}}"
  PROCESSING_API: "{{PROCESSING_API}}"

jobs:
- id: adaptive-processing
  name: adaptive-processing
  steps:
    - name: Assess Data Quality
      id: quality-check
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/data-quality"
      test: res.code == 200
      outputs:
        quality_score: res.body.quality_score
        has_errors: res.body.error_count > 0
        record_count: res.body.record_count

    - name: Standard Processing
      id: standard-processing
      uses: http
      skipif: outputs['quality-check'].quality_score < 0.8
      with:
        url: "{{vars.PROCESSING_API}}/standard"
        method: POST
        body: |
          {
            "record_count": {{outputs['quality-check'].record_count}},
            "quality_mode": "standard"
          }
      test: res.code == 200
      outputs:
        standard_result: res.body.result
        standard_time: (rt.sec * 1000)

    - name: Enhanced Processing
      id: enhanced-processing
      uses: http
      skipif: outputs['quality-check'].quality_score >= 0.8
      with:
        url: "{{vars.PROCESSING_API}}/enhanced"
        method: POST
        body: |
          {
            "record_count": {{outputs['quality-check'].record_count}},
            "quality_mode": "enhanced",
            "error_correction": true
          }
      test: res.code == 200
      outputs:
        enhanced_result: res.body.result
        enhanced_time: (rt.sec * 1000)
        corrections_applied: res.body.corrections

    - name: Processing Summary
      uses: hello
      echo: |
        Data Processing Complete:

        Quality Score: {{outputs['quality-check'].quality_score}}
        Processing Mode: {{outputs['quality-check'].quality_score >= 0.8 ? "Standard" : "Enhanced"}}

        {{outputs.standard_time != nil ? "Standard Processing Time: " + string(outputs.standard_time) + "ms" : ""}}
        {{outputs.enhanced_time != nil ? "Enhanced Processing Time: " + string(outputs.enhanced_time) + "ms" : ""}}
        {{outputs.corrections_applied != nil ? "Corrections Applied: " + string(outputs.corrections_applied) : ""}}
```

### データ蓄積パターン

複数のソースからデータを収集します：

```yaml
vars:
  SOURCE_A_URL: "{{SOURCE_A_URL}}"
  SOURCE_B_URL: "{{SOURCE_B_URL}}"
  SOURCE_C_URL: "{{SOURCE_C_URL}}"

jobs:
- id: multi-source-data-collection
  name: multi-source-data-collection
  steps:
    - name: Source A Data
      id: source-a
      uses: http
      with:
        method: GET
        url: "{{vars.SOURCE_A_URL}}/data"
      test: res.code == 200
      outputs:
        source_a_count: res.body.count
        source_a_data: res.body.data
        source_a_time: (rt.sec * 1000)

    - name: Source B Data
      id: source-b
      uses: http
      with:
        method: GET
        url: "{{vars.SOURCE_B_URL}}/data"
      test: res.code == 200
      outputs:
        source_b_count: res.body.count
        source_b_data: res.body.data
        source_b_time: (rt.sec * 1000)

    - name: Source C Data
      id: source-c
      uses: http
      with:
        method: GET
        url: "{{vars.SOURCE_C_URL}}/data"
      test: res.code == 200
      outputs:
        source_c_count: res.body.count
        source_c_data: res.body.data
        source_c_time: (rt.sec * 1000)

    - name: Aggregate All Sources
      uses: hello
      echo: |
        Multi-Source Data Summary:

        Source A: {{outputs['source-a'].source_a_count}} records ({{outputs['source-a'].source_a_time}}ms)
        Source B: {{outputs['source-b'].source_b_count}} records ({{outputs['source-b'].source_b_time}}ms)
        Source C: {{outputs['source-c'].source_c_count}} records ({{outputs['source-c'].source_c_time}}ms)

        Total Records: {{outputs['source-a'].source_a_count + outputs['source-b'].source_b_count + outputs['source-c'].source_c_count}}
        Average Response Time: {{(outputs['source-a'].source_a_time + outputs['source-b'].source_b_time + outputs['source-c'].source_c_time) / 3}}ms

        Fastest Source: {{
          outputs['source-a'].source_a_time <= outputs['source-b'].source_b_time && outputs['source-a'].source_a_time <= outputs['source-c'].source_c_time ? "Source A" :
          outputs['source-b'].source_b_time <= outputs['source-c'].source_c_time ? "Source B" : "Source C"
        }}
```

## データ検証と品質

出力が空だったり壊れていたりすると、生成したステップではなく、それを読むステップが失敗します。出力を検証して整えておけば、これを防げます。

### 出力検証

出力のデータ品質を確保します：

```yaml
- name: Data Collection with Validation
  id: validated-data
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/user-data"
  test: |
    res.code == 200 &&
    res.body.users != nil &&
    len(res.body.users) > 0 &&
    all(res.body.users, #.id != nil && #.email != nil)
  outputs:
    # 検証済み出力
    user_count: len(res.body.users)
    valid_users: 'filter(res.body.users, #.id != nil && #.email != nil)'
    admin_users: 'filter(res.body.users, #.role == "admin")'

    # データ品質メトリクス
    data_completeness: 'len(filter(res.body.users, #.id != nil && #.email != nil)) / len(res.body.users)'
    has_admin_users: 'any(res.body.users, #.role == "admin")'

    # レスポンスメタデータ
    data_freshness: res.headers["Last-Modified"]
    cache_status: res.headers["X-Cache-Status"]
```

### データサニタイズ

使用前にデータをクリーンアップ・サニタイズします：

```yaml
- name: Sanitize User Input
  id: sanitized-input
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/user-input"
  outputs:
    # 生データ
    raw_input: res.body.input

    # サニタイズ済みデータ
    clean_email: trim(lower(res.body.input.email))
    clean_name: trim(res.body.input.name)
    safe_description: res.body.input.description[0:500]  # 長さを制限

    # バリデーションフラグ
    email_valid: res.body.input.email matches "[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}"
    name_valid: len(res.body.input.name) >= 2 && len(res.body.input.name) <= 50
```

## パフォーマンス考慮事項

出力は実行が終わるまで保持されます。そのため、ステップが何を保存するか、後続のステップがどう参照するかがどちらも影響します。

### 効率的なデータアクセス

データアクセスパターンを最適化します：

```yaml
# 良い例: 直接的なプロパティアクセス
outputs:
  user_id: res.body.user.id
  user_name: res.body.user.name

# 良い例: 再利用を伴う単一計算
outputs:
  active_users: 'filter(res.body.users, #.active == true)'
  active_user_count: 'len(filter(res.body.users, #.active == true))'

# 避ける: 繰り返しの高コスト計算
# outputs:
#   user_count: len(filter(res.body.users, expensive_validation(#)))
#   user_list: filter(res.body.users, expensive_validation(#))
```

### メモリ管理

大きなデータセットに注意します：

```yaml
# 良い例: 必要不可欠なデータのみを抽出
outputs:
  user_ids: 'map(res.body.users, #.id)'
  user_count: len(res.body.users)
  first_user: res.body.users[0]

# 避ける: 大きなオブジェクトを不要に保存
# outputs:
#   all_user_data: res.body.users  # 非常に大きくなる可能性
#   complete_response: res.body     # レスポンス全体
```

### 選択的データ抽出

必要なデータのみを抽出します：

```yaml
- name: Efficient Data Extraction
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/large-dataset"
  outputs:
    # 概要情報のみを抽出
    record_count: res.body.metadata.total_records
    last_updated: res.body.metadata.last_updated
    status: res.body.metadata.status

    # 基準による特定レコードを抽出
    critical_items: 'filter(res.body.data, #.priority == "critical")'
    error_items: 'filter(res.body.data, #.status == "error")'

    # 集計を計算
    avg_score: 'sum(map(res.body.data, #.score)) / len(res.body.data)'
    max_score: 'max(map(res.body.data, #.score))'

    # データセット全体は保存しない
    # full_dataset: res.body.data  # 大きなデータセットでは避ける
```

## ベストプラクティス

以下では、出力の命名、型の一貫性、安全な参照方法、どのステップがどの出力に依存するかの記録を扱います。

### 1. 明確な出力命名

出力には説明的な名前を使用します：

```yaml
# 良い例: 説明的な名前
outputs:
  user_authentication_token: res.body.access_token
  session_expiry_timestamp: res.body.expires_at
  user_permission_level: res.body.user.role

# 避ける: 汎用的な名前
outputs:
  token: res.body.access_token
  time: res.body.expires_at
  level: res.body.user.role
```

### 2. 型整合性のある出力

一貫したデータタイプを維持します：

```yaml
# 良い例: 一貫したタイプ
outputs:
  user_count: len(res.body.users)          # 常に数値
  is_admin: res.body.user.role == "admin"    # 常にブール値
  user_email: res.body.user.email ?? ""      # 常に文字列（デフォルト値付き）

# 避ける: 不整合なタイプ
outputs:
  user_count: res.body.user_count ?? "unknown"  # 数値または文字列
```

### 3. エラーセーフなデータアクセス

潜在的なnull/undefined値を処理します：

```yaml
# 良い例: 安全なデータアクセス
outputs:
  user_id: 'res.body.user != nil ? res.body.user.id : nil'
  email_verified: res.body.user != nil && res.body.user.email_verified == true
  profile_complete: res.body.user?.profile != nil && res.body.user.profile.complete == true

# 良い例: 安全なナビゲーションを使用
test: res.body.user?.id != nil && res.body.user?.email != nil
```

### 4. データ依存関係の文書化

データがどこからどこに流れるかを文書化します：

```yaml
vars:
  api_url: "{{API_URL}}"
  smtp_addr: "{{SMTP_ADDR}}"

jobs:
- id: user-setup
  name: User Account Setup
  steps:
    - name: Create User Account
      id: user-setup
      uses: http
      with:
        method: POST
        url: "{{vars.api_url}}/users"
        body: |
          {"username": "probe_{{random_str(8)}}"}
      test: res.code == 201
      # 生成: user_id, username, email
      outputs:
        user_id: res.body.user.id
        username: res.body.user.username
        email: res.body.user.email

- id: user-verification
  name: User Account Verification
  needs: [user-setup]
  steps:
    - name: Send Verification Email
      # 使用: user-setup からの user_id, email
      uses: smtp
      with:
        addr: "{{vars.smtp_addr}}"
        from: "probe@example.com"
        to: "{{outputs['user-setup'].email}}"
        subject: "Verify your account"
        session: 1
        message: 1
        length: 500
      test: res.code == 0
      echo: "Click here to verify user {{outputs['user-setup'].user_id}}"
```

## 次のステップ

データフローを理解したら、以下を探索してください：

1. **[テストとアサーション](/ja/guide/concepts/testing-and-assertions)** - 検証技術を学ぶ
2. **[エラーハンドリング](/ja/guide/concepts/error-handling)** - データフローの失敗を適切に処理する
3. **[ハウツー](/ja/guide/how-tos/api-testing)** - 実用的なデータフローパターンを見る

データフローは、Probeワークフローのステップやジョブの間で値を渡す仕組みです。これらのパターンをマスターして、高度でデータ駆動の自動化プロセスを構築しましょう。
