---
title: クイックスタート
description: 5分でProbeを使い始める
weight: 20
---

# クイックスタート

このガイドでは、数分でProbeを使い始めることができます。最初のワークフローを作成し、Probeの動作を確認しましょう。

## 前提条件

- システムにProbeが[インストール](/ja/guide/introduction/installation)されていること
- YAML構文の基本的な理解

## 最初のワークフロー

ウェブサイトが正しく応答しているかをチェックする簡単なワークフローを作成しましょう。

### 1. ワークフローファイルの作成

`my-first-workflow.yml`という新しいファイルを作成します：

```yaml
name: My First Website Check
description: Check if my website is responding correctly

jobs:
- id: health-check
  name: Website Health Check
  steps:
    - name: Check Homepage
      uses: http
      with:
        url: https://httpbin.org/status/200
        method: GET
      test: res.code == 200

    - name: Check API Endpoint
      uses: http
      with:
        url: https://httpbin.org/json
        method: GET
      test: res.code == 200 && res.body.slideshow != nil

    - name: Success Message
      uses: hello
      echo: "✅ All checks passed! Website is healthy."
```

### 2. ワークフローの実行

Probe CLIを使用してワークフローを実行します：

```bash
$ probe my-first-workflow.yml
```

以下のような出力が表示されるはずです：

```
My First Website Check
Check if my website is responding correctly

⏺ Website Health Check (Completed in 1.23s)
  ⎿ 0. ✔︎  Check Homepage (245ms)
     1. ✔︎  Check API Endpoint (189ms)
     2. ✔︎  Success Message
       ✅ All checks passed! Website is healthy.

Total workflow time: 1.23s ✔︎ All jobs succeeded
```

### 3. 実行内容の理解

このワークフローが何をしているかを詳しく見てみましょう：

1. **ワークフローメタデータ**: `name`と`description`でワークフローの内容を説明
2. **ジョブ定義**: `health-check`ジョブに実行するステップをまとめる
3. **HTTPアクション**: 2つのステップでHTTPリクエストを行い、エンドポイントをテスト
4. **テストアサーション**: 各HTTPステップにレスポンスを検証する`test`を含む
5. **エコー出力**: すべてのチェックが通ったときに、`echo`で成功メッセージを表示

## 詳細出力の追加

何が起こっているかの詳細を見たい場合は、詳細フラグを付けて実行します：

```bash
$ probe my-first-workflow.yml --verbose
```

これにより、各HTTPリクエストとレスポンスの詳細情報が表示されます。

## 次のステップ

最初のワークフローを実行したので、次の内容に進むことができます：

1. **[核となる概念を学ぶ](/ja/guide/introduction/understanding-probe)** - ワークフロー、ジョブ、ステップを理解する
2. **[最初のカスタムワークフローを作成](/ja/guide/introduction/your-first-workflow)** - ニーズに合わせた独自のワークフローを構築する
3. **[CLIオプションを探る](/ja/guide/introduction/cli-basics)** - 利用可能なすべてのコマンドライン・オプションを学ぶ

## トラブルシューティング

最初の実行で起きる失敗は、ほぼ3つに集約されます。

### ワークフローファイルが見つからない

指定したパスがファイルを指していません。

```
[ERROR] workflow is required
```

YAMLファイルへの正しいパスを指定していることを確認してください。

### 無効なYAML構文

ファイルは見つかったものの、パースできていません。

```
[ERROR] yaml: line 5: mapping values are not allowed in this context
```

YAML構文を確認してください。よくある問題：
- 不正なインデント（タブではなくスペースを使用）
- キーの後のコロンの欠落
- 特殊文字を含む引用符なしの文字列

### テストの失敗

テストが失敗した場合は、詳細モード（`--verbose`）を使用して実際のレスポンスデータを確認し、テスト式をデバッグしてください。

さらに深く学ぶ準備はできましたか？[Probeを理解する](/ja/guide/introduction/understanding-probe)で核となる概念を学びましょう。
