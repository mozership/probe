# Probeとは？

Probeは、YAMLでワークフローを宣言的に定義できる無料のオープンソースツールです。軽量で単一バイナリとして動作し、複雑なインストールや設定は必要ありません。

## 何ができるのか？

Probeは以下のような用途で活用できます：

- **APIテスト**: REST APIのエンドツーエンドテストの自動化
- **シナリオテスト**: サービスに対する複合的な操作検証
- **実験定義**: 情報工学やソフトウェア工学などの研究実験の再現性向上
- **ウェブサイト監視**: サイトの稼働状況やパフォーマンスの監視
- **ヘルスチェック**: システムやサービスの健全性チェック
- **データ処理**: 定期的なデータ取得や変換処理
- **通知送信**: メールやSlackなどへの自動通知
- **タスク自動化**: 繰り返し行う作業の自動化

## 主な特徴

Probeが選ばれる理由となる、5つの主要な特徴をご紹介します。

### シンプルで直感的
YAMLベースの設定で、プログラミング知識がなくても簡単にワークフローを作成できます。

### 軽量・高速
Go言語で開発された単一バイナリで、依存関係なしに動作します。

### 拡張可能
Probe本体に手を入れずに、カスタムアクションを追加できます。

### 並行実行
複数のジョブを並行実行することで、効率的にワークフローを処理します。

### 豊富な機能
条件分岐、データ共有、エラーハンドリング、環境変数サポートなど、実用的な機能を搭載。

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

Probeには以下のビルトインアクションが用意されており、そのまま使用できます。現在利用可能なアクションと今後の開発予定をご紹介します。

### HTTP Action
REST APIの呼び出し、ウェブサイトの監視、APIテストなどに使用します。
- GET、POST、PUT、DELETE等のHTTPメソッドをサポート
- リクエストヘッダー、ボディの設定が可能
- レスポンスの検証とテスト機能

### SMTP Action
メール送信による通知や報告に使用します。
- SMTPサーバー経由でのメール送信
- HTML/テキストメールの対応
- 添付ファイルの送信

### Database Action
データベースの操作をすることができます。
- MySQL、PostgreSQL、SQLiteに対応
- SELECT、INSERT、UPDATE、DELETE等のSQL実行
- プリペアドステートメントによる安全なクエリ実行
- 実行結果の行数、影響を受けた行数の取得

### Shell Action
シェルコマンドの実行によって外部ソフトウェアとの連携ができます。
- bashやzshなど任意のシェルでコマンド実行
- 作業ディレクトリや環境変数の設定が可能
- 終了コード、標準出力・標準エラーの取得
- タイムアウト設定による実行時間の制御

### Browser Action
外部アクションとして[mozership/probe-browser](https://github.com/mozership/probe-browser)で公開しています。v1.21.0までは組み込みアクションでした。[Browser](/ja/reference/actions/browser)を参照してください。
Chrome DevTools Protocolを使ってChromeを操作することができます。
- ページの読み込み、要素のクリック、テキスト入力等の自動化
- スクリーンショットの撮影機能
- 要素の表示・非表示待機
- ヘッドレス・フルブラウザモードの選択

### Embedded Job Action
Jobを共通化し再利用することでWorkflowの運用を楽にします。
- 他のワークフローで定義したジョブを呼び出し
- パラメータの受け渡しが可能
- 複雑なワークフローの分割と再利用
- DRY原則に基づく効率的なワークフロー設計

> [!TIP]
>
> **開発予定のアクション**
>
> - **SSH Action** - リモートサーバーでのコマンド実行
> - **gRPC Action** - gRPCサービスとの通信
> - **FTP Action** - FTPサーバーとのファイル転送
> - **IMAP Action** - メールの受信と処理

### カスタムアクション

ビルトインアクションで要件を満たせない場合は、**カスタムアクション**を開発できます。Go言語で実装でき、外部アクションとして使うことも、自分でビルドするProbeに組み込むこともできます。

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
