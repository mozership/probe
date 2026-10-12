# インストール

Probeは実行時の依存がない単体のバイナリです。
最も短い手順はHomebrewです。
コンテナやコーディングエージェントのサンドボックスのようにHomebrewがない環境では、Goか、リリースをダウンロードする数行のコマンドで同じことができます。

## システム要件

- **オペレーティングシステム**: Linux、macOS
- **アーキテクチャ**: amd64、arm64
- **依存関係**: なし（静的リンクされたバイナリ）

## インストール方法

以下の方法から、他のツールの導入方法に合うものを選んでください。

### 1. Homebrew

```bash
brew install mozership/tap/probe
```

caskは[mozership/homebrew-tap](https://github.com/mozership/homebrew-tap)にあり、リリースのたびに、アーカイブのSHA-256とともに書き出されます。
Homebrewはインストールの前にそのSHA-256を検証します。
更新は`brew upgrade`で行えます。

### 2. Goでインストール

Go 1.26.6以降がインストールされている場合、Probeを直接インストールできます。

```bash
go install github.com/mozership/probe/cmd/probe@latest
```

`probe`バイナリは`$GOPATH/bin`ディレクトリにインストールされます。

### 3. リリースのダウンロード

アーカイブは、`checksums.txt`と一緒に[リリースページ](https://github.com/mozership/probe/releases)にあります。

- **Linux amd64**: `probe_linux_x86_64.tar.gz`
- **Linux arm64**: `probe_linux_arm64.tar.gz`
- **macOS amd64**: `probe_darwin_x86_64.tar.gz`
- **macOS arm64**: `probe_darwin_arm64.tar.gz`

次のコマンドは、最新のアーカイブをダウンロードして`checksums.txt`と照合し、`probe`をPATHが通ったディレクトリに置きます。
何も尋ねないので、Dockerfileやサンドボックスのセットアップスクリプトにそのまま書けます。

```bash
curl -fsSLO https://github.com/mozership/probe/releases/latest/download/probe_linux_x86_64.tar.gz
curl -fsSLO https://github.com/mozership/probe/releases/latest/download/checksums.txt
sha256sum -c --ignore-missing checksums.txt
tar -xzf probe_linux_x86_64.tar.gz probe
sudo mv probe /usr/local/bin/probe
```

macOSでは`shasum -a 256 -c --ignore-missing checksums.txt`で照合します。
最新以外のリリースを入れるには、`latest/download`を`download/v1.26.0`のように置き換えます。

### 4. GitHub Actions

[`mozership/probe-action`](https://github.com/mozership/probe-action)は、Probeをダウンロードしてチェックサムを検証し、キャッシュしたうえで、ワークフローを実行します。

```yaml
- uses: actions/checkout@v4
- uses: mozership/probe-action@v1.3.0
  with:
    path: probe/api.yml
    options: --report github-summary
```

`install-only: true`を指定すると、`probe`を`PATH`に入れるだけで終わるので、後続のステップから自分で実行できます。

### 5. ソースからビルド

Probeをソースからビルドするには、次のようにします。

```bash
git clone https://github.com/mozership/probe.git
cd probe
go build -o probe ./cmd/probe
sudo mv probe /usr/local/bin/
```

## インストールの確認

インストール後、Probeが動くことを確認します。

```bash
probe --version
```

次のような出力が表示されます。
```
Probe Version 1.26.0 (commit: 77d72480c9bbc97cf32e202e69d9af6fdd9d1098)
```

## コーディングエージェントのために

新しいサンドボックスで作業するエージェントは、Probeを自分でインストールする必要があります。
`AGENTS.md`や`CLAUDE.md`など、エージェントが指示を読むファイルに、上のインストールコマンドのどれかを書いておきます。
あわせてプロジェクトにスキルを入れておくと、エージェントはワークフローの書き方をそこから学びます。

```bash
probe skill install
```

エージェントに与える指示の例は、[コーディングエージェントと使う](/ja/guide/how-tos/coding-agents)にあります。

## 次のステップ

Probeのインストールが完了したら、次の内容に進みましょう。

1. **[Probeを理解する](/ja/guide/introduction/understanding-probe)** - 核となる概念を理解する
2. **[CLIの操作](/ja/guide/introduction/cli-basics)** - 基本的なコマンドやオプションを知る
3. **[最初のワークフロー](/ja/guide/introduction/your-first-workflow)** - 簡単な例から始める

## トラブルシューティング

インストール時の問題は、ファイルの実行権限か、シェルが探す場所のどちらかであることがほとんどです。

### Permission Denied

Linux/macOSで「permission denied」エラーが出る場合は、実行権限を付けます。

```bash
chmod +x probe
```

### Command Not Found

`probe`コマンドが見つからない場合、バイナリがPATHに含まれていることを確認します。

```bash
echo $PATH
which probe
```

### Apple Silicon上のARM64

Apple SiliconのMacでは、Homebrewがarm64のアーカイブを選びます。
手でダウンロードするときは、x86_64のアーカイブではなく`probe_darwin_arm64.tar.gz`を選んでください。
