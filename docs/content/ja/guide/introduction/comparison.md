# 類似ソフトウェアとの比較

Probeはワークフロー実行ツールで、テストや監視はその用途のひとつです。目的の異なる類似のソフトウェアが複数あり、どれを選ぶかは何をしたいかで決まります。

## 一覧

表では、各ツールの種別、記述形式、操作できる対象、実行に必要なものを並べています。

| | 種別 | 記述形式 | 操作対象 | 実行形態 |
|---|---|---|---|---|
| **Probe** | ワークフロー実行 | YAML | HTTP、DB、SMTP、IMAP、DNS、SSH、シェル、ブラウザ、gRPC、WebSocket、Redis、S3 | 単体のGoバイナリ |
| **k6** | 負荷試験 | JavaScript | HTTP、gRPC、WebSocket | 単体のGoバイナリ |
| **Postman / Newman** | APIテスト | GUIとコレクション | HTTP | GUIアプリとNode.js |
| **Hurl** | HTTPテスト | 独自のテキスト形式 | HTTP | 単体のRustバイナリ |
| **Venom** | テスト実行 | YAML | HTTP、SMTP、IMAP、SSH、SQL、gRPCなど | 単体のGoバイナリ |
| **runn** | シナリオ実行 | YAML（runbook） | HTTP、gRPC、DB、ブラウザ、SSH、コマンド実行 | 単体のGoバイナリ、Goのテストからも利用可 |
| **Blackbox exporter** | 外形監視 | 設定ファイル | HTTP、TCP、DNS、ICMP | 常駐プロセス |
| **GitHub Actions** | CI | YAML | ランナー上で動かせるもの | GitHubのランナー |
| **CWL / WDL** | 計算パイプライン記述 | YAML・JSON（CWL）、独自のDSL（WDL） | コンテナ内のコマンドとファイル | cwltool、Cromwell、miniwdlなどのエンジン |

## それぞれとの違い

表で示したのは各ツールの概要です。ここからは1つずつ取り上げ、Probeと重なる範囲と、他方を選んだほうがよい範囲を示します。

### k6

[k6](https://k6.io/)は負荷をかけて性能を測ることが目的です。同時実行数やレスポンスタイムの分布といった指標を扱います。Probeには負荷をかける仕組みがなく、1回の流れが正しく通るかを検証するものです。性能を測りたいならk6を使ってください。

### Postman / Newman

[Postman](https://www.postman.com/)とそのCLIである[Newman](https://github.com/postmanlabs/newman)はHTTPが中心で、実行にNode.jsが必要です。コレクションはGUIで編集する前提の形式で、差分を読むのには向きません。ProbeはHTTP以外のプロトコルを同じファイルに混ぜられ、YAMLなのでレビューできます。

### Hurl

[Hurl](https://hurl.dev/)はHTTPに絞られていて、その範囲では記述が最も短くなります。ProbeはHTTP以外も扱う代わりに、Jobとステップの構造を書く分だけ記述が長くなります。HTTPだけを検査するならHurlのほうが記述が短く済みます。

### Venom

[Venom](https://github.com/ovh/venom)も近いツールです。YAMLで複数のプロトコルを扱い、単体バイナリで動きます。

Probeとの違いは2点あります。ひとつはJob間の依存を`needs`で宣言し、その依存グラフを`probe dag`で図として出力できること。もうひとつは`repeat`で同じファイルを一定間隔で繰り返し実行し、成功率を返せることです。後者により、テストとして書いたものをそのまま監視に転用できます。

### runn

[runn](https://github.com/k1LoW/runn)はProbeに最も近いツールのひとつです。YAMLでシナリオを書き、HTTP、gRPC、DB、ブラウザ、SSH、コマンド実行を同じファイルに並べられます。式の評価にexprを使っている点もProbeと同じで、`test`に書ける条件の書き方はよく似ています。

違いは3点あります。runnはrunbookを上から順に実行しますが、ProbeはJobを単位として並列に実行し、依存関係を`needs`で宣言します。runnはGoのテストコードから呼び出してテストヘルパーとして使えますが、ProbeはCLIとして動きます。そしてProbeのアクションはgo-plugin越しの別プロセスなので、本体に手を入れずに追加できます。

### Blackbox exporter

[Blackbox exporter](https://github.com/prometheus/blackbox_exporter)は単発のプローブを繰り返す外形監視で、Prometheusから収集される前提で作られています。ログインしてトークンを受け取り、次のリクエストに渡すといった多段の流れは扱いません。Probeは多段の流れを書けますが、メトリクスを収集して蓄積する仕組みは持ちません。

### GitHub Actions

[GitHub Actions](https://docs.github.com/actions)は記法が近く、`jobs`、`steps`、`needs`、`uses`、`with`という構造はProbeとよく似ています。ただしGitHubのランナー上で動くものなので、手元のターミナルで実行することも、依存グラフを表示することもできません。ProbeはCIの中でも手元でも同じように動きます。


### CWL / WDL

同じ「ワークフロー」という語を使いますが、指すものが違います。[Common Workflow Language](https://www.commonwl.org/)と[Workflow Description Language](https://openwdl.org/)は計算処理のパイプラインを記述するための仕様で、バイオインフォマティクスをはじめとする研究分野で使われています。

各ステップはコンテナの中でコマンドを実行し、ファイルを入出力します。依存関係は`needs`のような宣言ではなく、あるステップの出力を別のステップの入力に繋ぐことで決まります。実行を担うのは仕様そのものではなくcwltoolやCromwell、miniwdlといったエンジンで、HPCのジョブスケジューラやクラウドのバッチ基盤に投げることを前提にしています。

Probeは動いているシステムを操作して応答を検査するもので、データを加工するパイプラインではありません。大量のファイルを段階的に処理する用途にProbeは向きません。

## 注意

この比較は2026年9月時点での各ツールの構造に基づいています。それぞれ更新されていくので、採用を判断する前に各プロジェクトの最新のドキュメントを確認してください。

## 関連項目

- **[Probeとは？](/ja/guide/introduction/what-is-probe)** - できることと主な特徴
- **[Probeを理解する](/ja/guide/introduction/understanding-probe)** - 実行モデルと設計
