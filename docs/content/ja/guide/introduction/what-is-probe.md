# Probeとは？

Probeは、ソフトウェアが動くことをEnd-to-endで確かめる、無料のオープンソースツールです。
YAMLで書いたワークフローが、動いているサービスをHTTP、gRPC、SQL、メールなど、クライアントが使うのと同じプロトコルで操作し、応答を1つずつ検証します。
Probeは単体のバイナリで、手元でも、CIでも、cronからでも同じように動きます。

## いま必要とされる理由

AIエージェントはサービスを変更するとき、コードとユニットテストを課題の同じ解釈から書きます。
そのため、解釈が誤っていても両方が通ります。
変更が動くことを示すものは、そのコードの外側から得る必要があります。
動いているサービスに送ったリクエスト、書き込まれた行、送信されたメールがそれにあたります。

Probeは、その確認を行うために、そしてAIエージェント自身がワークフローを書けるように作られています。

- `probe skill`は、インストールされているバージョンに合うワークフローの書き方をAIエージェントに教えます。`probe check`は、実行する前に書き間違いを見つけます。
- ステップはOpenAPIドキュメントや`.proto`ファイルと照合できます。正しさを決めるのは、AIエージェントが書いた`test`ではなく仕様です。
- `--read-only`と`--allow-host`は、書き込みと、指定していないホストへの接続を、送信する前に拒否します。
- レポートには、サービスが何をしたかがステップごとに載るので、変更をレビューする人が読めます。

次の例では、AIエージェントがワークフローとして書いたE2Eテストを検査し、サインアップAPIとそのデータベースに対して実行し、最後に`--read-only`でもう一度実行しています。

<img class="diagram" src="/demo-ja.svg" alt="3場面の端末。probe checkがAIエージェントの書いたE2Eテストの誤りを報告し、直したテストがAPIを呼んで書き込まれた行を問い合わせ、--read-onlyではPOSTが拒否される。" />

この形でプロジェクトを設定する手順は、[AIエージェントと使う](/ja/guide/how-tos/coding-agents)にあります。

## 何ができるのか？

ワークフローは1つのファイルなので、変更を確かめるために書いたものを、その後も使い続けられます。

- **End-to-endテスト**：エンドポイントを呼び、書き込まれた行を問い合わせ、送信されたメールを読むまでを1回の実行で行う
- **APIテスト**：リクエストと応答を、検証式とOpenAPIドキュメントの両方で確かめる
- **監視**：`repeat`で同じワークフローを一定の間隔で実行し、成功率を報告する
- **スモークテストとヘルスチェック**：終了コードでデプロイを止める
- **再現できる実験**：複数のプロトコルにまたがる手順を書き残し、同じ手順で何度でも実行する

## 主な特徴

同種のツールと比べたときのProbeの特徴は次のとおりです。
ほかのツールを選んだほうがよい場合は、[類似ソフトウェアとの比較](/ja/guide/introduction/comparison)で説明しています。

### 複数のプロトコルを1回の実行で
HTTP、gRPC、MySQL、PostgreSQL、SQLite、SMTP、IMAP、DNS、SSH、シェルが組み込みです。ジョブは`needs`で順序を決めない限り並行して動きます。

### 読みやすい
ワークフローは、`jobs`、`steps`、`uses`、`with`で構成する、GitHub Actionsに近いYAMLです。AIエージェントが書いたものを人がレビューできます。

### AIエージェントへの対応
スキル、`probe check`、ガード、仕様との照合がバイナリに含まれています。

### 単体のバイナリ
Goで開発されており、実行時の依存がありません。1つのコマンドでインストールできます。

### 拡張可能
新しいプロトコルは、Probe本体を変更せず、独立したアクションとして追加できます。

## 簡単なワークフローの例

実際のコードを見ながらProbeの使い方を理解しましょう。

### 基本的なHello Worldワークフロー

まずは最もシンプルな例から始めましょう。以下のYAMLファイルを作成します：

```yaml
name: My Workflow

jobs:
- name: My First Job
  steps:
  - name: Say Hello
    uses: shell
    with:
      cmd: echo "Hello, World!"
    test: res.code == 0
    echo: "{{ res.stdout }}"
```

このワークフローをファイルに保存して実行します：

```bash
$ probe hello-world.yml
My Workflow

⏺ My First Job (Completed in 0.02s)
  ⎿  0. ✔︎  Say Hello
           Hello, World!

Total workflow time: 0.02s ✔︎ All jobs succeeded
```

### より実践的な例：API監視

実際の使用例として、APIの稼働状況を監視するワークフローです：

```yaml
name: GitHub API

jobs:
- name: Check APIs
  defaults:
    http:
      url: https://api.github.com
      headers:
        content_type: application/json

  steps:
  - name: Check Repo API
    uses: http
    with:
      get: /repos/mozership/probe
    test: res.code == "200 OK" && res.body.language == "Go"

  - name: Check User API
    uses: http
    with:
      get: /users/linyows
    test: res.code == "200 OK" && res.body.id == 72049
```

このように、Probeを使えば**簡単**で**迅速**、そして**楽しく**ワークフローを作成・実行できます。

## 利用可能なアクション

次のアクションが組み込まれています。
それぞれのページは[リファレンス](/ja/reference/actions/variables)にあります。

| アクション | 用途 |
|---|---|
| [`http`](/ja/reference/actions/http) | HTTPリクエストとその応答 |
| [`grpc`](/ja/reference/actions/grpc) | gRPCとConnectの呼び出し（ストリーミングを含む） |
| [`db`](/ja/reference/actions/db) | MySQL、PostgreSQL、SQLiteへのクエリ |
| [`smtp`](/ja/reference/actions/smtp) | メールの送信 |
| [`imap`](/ja/reference/actions/imap) | メールボックスの読み取り |
| [`dns`](/ja/reference/actions/dns) | 名前に対してDNSサーバーが返すレコード |
| [`ssh`](/ja/reference/actions/ssh) | リモートホストでのコマンド実行 |
| [`shell`](/ja/reference/actions/shell) | Probeを実行しているマシンでのコマンド実行 |
| [`embedded`](/ja/reference/actions/embedded) | ステップから別のジョブファイルを実行 |
| [`hello`](/ja/reference/actions/hello) | レポートへの出力 |

外部アクションはそれぞれ独立したリポジトリにあり、ステップからコミットを指定して使います。
[browser](/ja/reference/actions/browser)、[graphql](/ja/reference/actions/graphql)、[jmap](/ja/reference/actions/jmap)、[mail-latency](/ja/reference/actions/mail-latency)、[redis](/ja/reference/actions/redis)、[s3](/ja/reference/actions/s3)、[websocket](/ja/reference/actions/websocket)があります。

### カスタムアクション

必要なプロトコルを扱うアクションがなければ、Goで書いて外部アクションとして公開できます。
詳しくは[アクション](/ja/guide/concepts/actions)を参照してください。

## 始めてみましょう

Probeを始めるのは簡単です。以下の3つのステップで、すぐにワークフローを作成・実行できます：

1. **[インストール](/ja/guide/introduction/installation)** - 環境に応じた方法でProbeをインストール
2. **[クイックスタート](/ja/guide/introduction/quickstart)** - 5分で最初のワークフローを作成・実行
3. **[基本概念を理解](/ja/guide/introduction/understanding-probe)** - ワークフロー、ジョブ、ステップの仕組みを学習

## 適用シーン

Probeは様々な業務領域で活用できます。主な適用シーンをご紹介します。

### DevOps・SRE
- CI/CDパイプラインでのスモークテスト
- サービス監視とアラート
- インフラストラクチャの健全性チェック

### API開発・テスト
- REST APIの統合テスト
- パフォーマンステスト
- エンドツーエンドテストの自動化

### 運用・保守
- 定期的なヘルスチェック
- システム間の連携確認
- 障害時の自動復旧処理

## 困った時は？

Probeの使用中に問題が発生した場合や質問がある場合の、サポートリソースをご案内します。

### ドキュメント
このウェブサイトには包括的なドキュメントが用意されています。基本的な使い方から高度な機能まで、すべてカバーしています。

### コミュニティサポート
質問、フィードバック、アイデアの議論には[GitHub Discussions](https://github.com/mozership/probe/discussions)をご利用ください。お気軽にディスカッションを始めてください。

### バグ報告
バグを発見した場合は、[GitHub Issues](https://github.com/mozership/probe/issues)で報告してください。

## プロジェクトを応援してください

このプロジェクトが役に立ったら、ぜひ応援してください！皆様のご支援が開発者にとって大きな励みとなります。

- [GitHubでスター](https://github.com/mozership/probe)を付ける
- Xでツイートして友人に紹介
- ブログや記事で紹介
- 機能改善のアイデアを提案

## オープンソースへの貢献

オープンソースプロジェクトへの貢献に興味がある方、今がチャンスです！コードの貢献、ドキュメントの改善、バグ報告など、あらゆる形での貢献を歓迎します。
