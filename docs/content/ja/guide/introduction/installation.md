# インストール

Probeは軽量でシングルバイナリのツールで、複数の方法でインストールできます。お使いの環境に最適な方法を選択してください。

## システム要件

- **オペレーティングシステム**: Linux、macOS
- **アーキテクチャ**: amd64、arm64
- **依存関係**: なし（静的リンクされたバイナリ）

## インストール方法

Probeは単一のバイナリです。以下の3つから、他のツールの導入方法に合うものを選んでください。

### 1. プリビルドバイナリのダウンロード

Probeをインストールする最も簡単な方法は、GitHubのリリースページからプリビルドバイナリをダウンロードすることです。

1. [Probeリリースページ](https://github.com/mozership/probe/releases)にアクセス
2. お使いのシステムに適したアーカイブをダウンロード：
   - **Linux amd64**: `probe_linux_x86_64.tar.gz`
   - **Linux arm64**: `probe_linux_arm64.tar.gz`
   - **macOS amd64**: `probe_darwin_x86_64.tar.gz`
   - **macOS arm64**: `probe_darwin_arm64.tar.gz`

3. アーカイブから`probe`バイナリを取り出す：
   ```bash
   tar -xzf probe_linux_x86_64.tar.gz
   ```

4. PATHが通ったディレクトリに移動：
   ```bash
   sudo mv probe /usr/local/bin/probe
   ```

### 2. Goでインストール

Go 1.26.6以降がインストールされている場合、Probeを直接インストールできます：

```bash
go install github.com/mozership/probe/cmd/probe@latest
```

これにより、`probe`バイナリが`$GOPATH/bin`ディレクトリにインストールされます。

### 3. ソースからビルド

Probeをソースからビルドするには：

```bash
git clone https://github.com/mozership/probe.git
cd probe
go build -o probe ./cmd/probe
sudo mv probe /usr/local/bin/
```

## インストールの確認

インストール後、Probeが正しく動作していることを確認します：

```bash
probe --version
```

以下のような出力が表示されるはずです：
```
Probe Version 1.13.0 (commit: 2d9d511ce7b4c9eec32ea85e2a25337e40a5e40c)
```

## 次のステップ

Probeのインストールが完了したら、次の内容に進みましょう：

1. **[Probeを理解する](/ja/guide/introduction/understanding-probe)** - 核となる概念を理解する
2. **[CLIの操作](/ja/guide/introduction/cli-basics)** - 基本的なコマンドやオプションを知る
3. **[最初のワークフロー](/ja/guide/introduction/your-first-workflow)** - 簡単な例から始める

## トラブルシューティング

インストール時の問題は、ファイルの実行権限か、シェルが探す場所のどちらかであることがほとんどです。

### Permission Denied

Linux/macOSで「permission denied」エラーが出る場合：

```bash
chmod +x probe
```

### Command Not Found

`probe`コマンドが見つからない場合、バイナリがPATHに含まれていることを確認します：

```bash
echo $PATH
which probe
```

### Apple Silicon上のARM64

Apple Silicon Mac（M1/M2）では、パフォーマンス向上のため`probe_darwin_arm64.tar.gz`を使用してください。
