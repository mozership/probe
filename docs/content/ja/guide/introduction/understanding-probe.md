---
title: Probeを理解する
description: Probeの核となる概念とアーキテクチャを学ぶ
weight: 30
---

# Probeを理解する

ProbeはYAMLベースのワークフロー自動化ツールで、監視、テスト、自動化タスクのために設計されています。このガイドでは、Probeを効果的に使用するために理解すべき核となる概念を説明します。

ProbeはYAMLで書いたワークフローを読み、検証してから、ワークフローエンジンで実行します。エンジンはジョブを順序付け、ステップを実行し、式を評価し、結果を表示します。各ステップはアクションを呼び出します。アクションは、Probeに組み込まれたものか、独立したリポジトリにある外部アクションです。

<img class="diagram diagram--light" src="/architecture.svg" alt="Probeのアーキテクチャ。YAMLのワークフローがパーサーとバリデーターを経てワークフローエンジンに渡ります。エンジンはスケジューラー、エグゼキューター、アクション、式、プリンターからなり、組み込みアクションと外部アクションを呼び出します" />
<img class="diagram diagram--dark" src="/architecture-dark.svg" alt="" />

## 核となる概念

Probeの構成要素は4つです。ワークフローがジョブを持ち、ジョブがステップを持ち、ステップがアクションを呼び出します。

### ワークフロー

**ワークフロー**は、Probeが実行すべき内容を定義するトップレベルのコンテナです。以下から構成されます：

- **メタデータ**: ワークフローの名前と説明
- **ジョブ**: 並行または順次実行される1つ以上のジョブ
- **グローバル設定**: すべてのジョブに適用される共有設定

```yaml
name: My Workflow
description: What this workflow does
# Jobs go here...
```

### ジョブ

**ジョブ**は一緒に実行されるステップの集合です。ジョブは以下が可能です：

- 他のジョブと並行実行
- 他のジョブに対する依存関係を持つ
- 他のジョブと出力を共有
- 独自の設定とコンテキストを持つ

```yaml
jobs:
- id: job-name
  name: Human-readable job name
  needs: [other-job]  # Optional: wait for other jobs
  steps:
    # Steps go here...
```

### ステップ

**ステップ**は実行の最小単位です。各ステップは以下が可能です：

- アクション（HTTPリクエスト、メールなど）を実行
- 結果を検証するテストを実行
- 出力にメッセージをエコー
- 他のステップで使用する出力を設定
- 条件付き実行ロジックを持つ

```yaml
steps:
  - name: Step Name
    action: http          # The action to execute
    with:                 # Parameters for the action
      url: https://api.example.com
      method: GET
    test: res.code == 200  # Test condition
    outputs:              # Data to pass to other steps
      response_time: (rt.sec * 1000)
```

### アクション

**アクション**は、ステップから呼ばれて実際の操作を行う部品です。Probeには組み込みアクションが含まれています：

- **`http`**: HTTP/HTTPSリクエストを作成
- **`hello`**: 簡単な挨拶アクション（主にテスト用）
- **`smtp`**: SMTP経由でメールを送信

アクションは共通のインターフェースを持つ別プロセスとして動くため、カスタムアクションでProbeを拡張できます。

## ワークフロー実行モデル

ジョブは依存関係を指定しない限り同時に実行され、あるジョブが生成したデータを次のジョブが読み取ります。

### 並行実行

デフォルトでは、ジョブは最大効率のために並行実行されます：

```yaml
jobs:
- id: frontend-check
  name: frontend-check
  # ...             # at the same time
- id: backend-check
  name: backend-check
  # ...
- id: database-check
  name: database-check
  # ...
```

### 依存関係による順次実行

`needs`キーワードを使用して依存関係を作成：

```yaml
jobs:
- id: setup
  name: Setup Environment
  steps:
    # Setup steps...

- id: test
  name: Run Tests
  needs: [setup]     # Wait for 'setup' to complete
  steps:
    # Test steps...

- id: cleanup
  name: Clean Up
  needs: [test]      # Wait for 'test' to complete
  steps:
    # Cleanup steps...
```

### データフロー

データは**出力**を使用してワークフロー内を流れます：

```yaml
jobs:
- id: data-fetch
  name: data-fetch
  steps:
    - name: Get User Info
      id: data-fetch
      uses: http
      with:
        method: GET
        url: https://api.example.com/user/123
      outputs:
        user_id: res.body.id
        user_name: res.body.name

- id: notification
  name: notification
  needs: [data-fetch]
  steps:
    - name: Send Welcome Email
      uses: hello
      echo: "Welcome {{outputs['data-fetch'].user_name}}!"
```

## 式システム

Probeは動的な値とテストのために式を使用します。式は`{{}}`構文を使って記述されます：

### テンプレート式

テンプレート式を使用して動的な値を挿入：

```yaml
- name: Greet User
  echo: "Hello {{outputs['previous-step'].username}}!"
```

### テスト式

テスト式を使用して結果を検証：

```yaml
- name: Check API Response
  uses: http
  with:
    method: GET
    url: https://api.example.com/status
  test: res.code == 200 && res.body.healthy == true
```

### 利用可能な変数

式では以下にアクセスできます：

- **`res`**: 現在のアクションからのレスポンス
- **`outputs`**: 前のステップ/ジョブからの出力
- **`env`**: 環境変数
- **カスタム関数**: `random_int()`、`random_str()`、`unixtime()`

## ファイルマージ

Probeは複数のYAMLファイルのマージをサポートしており、以下に有用です：

- ワークフローロジックから設定を分離
- ワークフロー間での共通定義の再利用
- 環境固有のオーバーライド

```bash
# ベースワークフローと環境固有の設定をマージ
probe base-workflow.yml,production-config.yml
```

ファイルは順に連結され、複数のファイルで定義されたトップレベルのキーは最後のファイルの値になります。

## エラーハンドリング

Probeはエラーを処理するためのいくつかのメカニズムを提供します：

### テストの失敗

テストが失敗すると、ステップは失敗としてマークされます：

```yaml
- name: Critical Check
  uses: http
  with:
    method: GET
    url: https://critical-api.example.com
  test: res.code == 200  # If this fails, step fails
```

### 条件付き実行

ステップをスキップするには`skipif`を使います。式からは先行ステップのoutputsが見えるので、判断材料は`test`で失敗させずにoutputsとして公開します。

```yaml
- name: Primary Service Check
  id: primary
  uses: http
  with:
    method: GET
    url: https://primary-api.example.com
  outputs:
    primary_ok: res.code == 200

- name: Fallback Check
  uses: http
  skipif: outputs.primary.primary_ok
  with:
    method: GET
    url: https://backup-api.example.com
  test: res.code == 200
```


### ステップが失敗したとき

`test`が失敗するとそのステップとジョブは失敗扱いになりますが、同じジョブの残りのステップは実行されます。失敗したジョブを`needs`に指定したジョブはスキップされ、ワークフローの終了ステータスは`1`になります。アクション自体がエラーを返した場合は`3`になります。

失敗を無視するためのステップ単位・ジョブ単位のスイッチはありません。ワークフロー全体を失敗させたくないチェックは、`test`で判定せず結果をoutputsとして公開します。

```yaml
- name: Optional Service Check
  id: optional
  uses: http
  with:
    method: GET
    url: https://optional-service.example.com
  outputs:
    optional_ok: res.code == 200
```


## ベストプラクティス

以下では、命名、ステップのまとめ方、データの受け渡し方、テストで検証する内容を扱います。

### 1. 説明的な名前を使用

名前はレポートに出力されます。失敗したときに読まれるのはこの名前です。

```yaml
# Good
- name: Check Production API Health
  uses: http
  # ...

# Not so good  
- name: HTTP Check
  uses: http
  # ...
```

### 2. 関連するステップをジョブにグループ化

対象が同じステップは1つのジョブにまとめます。ジョブは順に実行され、まとめて失敗する単位だからです。

```yaml
jobs:
- id: infrastructure-check
  name: Infrastructure Health Check
  steps:
    - name: Check Database
      # ...
    - name: Check Cache
      # ...
    - name: Check Load Balancer
      # ...
```

### 3. データ共有に出力を使用

後で必要になる値は、取得し直すのではなく出力として公開します。

```yaml
- name: Fetch Configuration
  id: config
  uses: http
  with:
    method: GET
    url: https://config-service.example.com
  outputs:
    database_url: res.body.database_url
    
- name: Test Database Connection
  uses: http
  with:
    method: GET
    url: "{{outputs.config.database_url}}/health"
```

### 4. 意味のあるテスト条件を追加

テストでは、応答が返ったことではなく、そのステップの目的が果たされたことを検証します。

```yaml
# Good - specific test conditions
test: res.code == 200 && res.body.status == "healthy" && (rt.sec * 1000) < 1000

# Not so good - generic test
test: res.code == 200
```

## 次のステップ

核となる概念を理解したので、次の内容に進むことができます：

1. **[最初のワークフローを作成](/ja/guide/introduction/your-first-workflow)** - 実践的なワークフローを構築
2. **[CLI基本を学ぶ](/ja/guide/introduction/cli-basics)** - コマンドライン・インターフェースをマスター
3. **[リファレンスを探る](/ja/reference/actions/variables)** - 利用可能なすべてのオプションを深く理解

Probeをマスターするには、実際にワークフローを書いて動かすことが欠かせません。シンプルなワークフローから始めて、概念に慣れてきたら徐々により複雑な自動化を構築していきましょう。
