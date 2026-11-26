# Slurmアクション

Slurmアクションは、[Slurm](https://slurm.schedmd.com/)にジョブを投入し、終わるまで待って出力を読み、取り消します。クラスターのパーティションとノードの状態も読みます。Slurmのコマンド（`sbatch`、`squeue`、`sacct`、`scancel`、`scontrol`）を、SSHでログインノードの上で、またはProbeを実行しているホストの上で実行し、`--json`で出力された内容を読みます。

[外部アクション](/ja/guide/concepts/actions#外部アクション)として[mozership/probe-slurm](https://github.com/mozership/probe-slurm)で公開しています。ワークフローが初めて使うときにProbeがダウンロードし、固定したコミットの`action.yml`が示すSHA-256と一致する実行ファイルだけを実行します。Probe v1.40.0以降で試験しています。例の`<commit SHA>`には、固定するコミットを書きます。最新リリースの`uses`の行は`probe guide slurm`が表示し、各リリースのノートにもあります。

## 基本的な構文

ワークフローでは40文字のコミットSHAでアクションを固定し、[`actions`](/ja/reference/yaml-configuration#actions)で一度だけ名前を付けます。

```yaml
name: Canary job
actions:
  slurm: github.com/mozership/probe-slurm@<commit SHA>
jobs:
- name: canary
  defaults:
    slurm:
      host: login.hpc.example.com
      user: probe
      key_file: ~/.ssh/id_ed25519
  steps:
  - name: The partition is up, with a node to spare
    uses: slurm
    with:
      partitions: debug
    test: res.partitions[0].state == "UP" && res.partitions[0].idle > 0

  - name: Submit a job
    id: submit
    uses: slurm
    with:
      submit:
        partition: debug
        time: 5m
        script: |
          #!/bin/bash
          srun hostname
    test: res.job_id > 0
    outputs:
      job_id: res.job_id

  - name: It ends well, soon enough
    uses: slurm
    with:
      job: "{{outputs.submit.job_id}}"
      wait: 30m
      output: true
    test: |
      res.state == "COMPLETED" && res.exit_code == 0 &&
      res.wait_sec < 600 && res.stdout != ""
```

ジョブの投入と待機は別のステップにします。待機するステップを`retry`で繰り返しても、ジョブを二重に投入しないようにするためです。

## パラメータ

ステップは、`with`の次のキーのどれか1つで1つのことをします。

| キー | すること | 値 |
|---|---|---|
| [`submit`](#submit) | `sbatch`でジョブを投入します | ジョブ |
| [`job`](#job) | ジョブの状態を読みます。`wait`を付けると終わるまで待ちます | ジョブのID |
| [`jobs`](#jobs) | キューのジョブを一覧します | ジョブの条件。なければすべてのジョブ |
| [`cancel`](#cancel) | `scancel`でジョブを取り消します | ジョブのID |
| [`partitions`](#partitions) | パーティションの状態を読み、そのノードを状態ごとに数えます | パーティションの名前。なければすべてのパーティション |
| [`nodes`](#nodes) | ノードの状態を読みます | ノード。`c1`や`c[1-4]`など。なければすべてのノード |

残りのパラメータは、コマンドを実行する場所を指定します。

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|---|---|---|---|---|
| `host` | String | No | - | コマンドをSSHで実行するログインノード。なければProbeを実行しているホストで実行します |
| `port` | Integer | No | `22` | SSHのポート |
| `user` | String | No | `USER` | ログインするユーザー |
| `key_file` | String | No | [ログイン](#ログイン)を参照 | ログインに使う秘密鍵。`~/`はホームディレクトリです |
| `key_passphrase` | String | No | - | `key_file`のパスフレーズ |
| `known_hosts` | String | No | `~/.ssh/known_hosts`と`/etc/ssh/ssh_known_hosts` | ログインノードのホスト鍵を照合するファイル |
| `bin_dir` | String | No | `PATH` | Slurmのコマンドがあるディレクトリ。`/opt/slurm/bin`など。SSHで実行するコマンドの`PATH`で見つからない場合に指定します |
| `timeout` | Duration | No | `30s`に`wait`を足した長さ | ステップ全体の制限時間。`10s`または秒数で指定します |

これら以外のキーを`with`に書くと、何も実行せずにステップが失敗します。`action.yml`がこれらを`params`として、結果のフィールドを`res`として宣言しているので、`probe check`はそのようなキーと、アクションが返さない`res`のフィールドを行番号つきで報告します。

ジョブの`defaults`を、ワークフローがアクションに付けた名前をキーにして書くと、キーごとにマージします。そのため、ログインノードや、ジョブを投入するときの指定を各ステップに書かずに済みます。

```yaml
defaults:
  slurm:
    host: login.hpc.example.com
    submit:
      partition: debug
      account: lab
```

この`defaults`のように`script`も`file`もない`submit`は、ほかのことをするステップでは無視します。

### ログイン

`key_file`があれば、その鍵でログインします。なければ、`SSH_AUTH_SOCK`のエージェントの鍵を試し、次に`~/.ssh/id_ed25519`、`~/.ssh/id_ecdsa`、`~/.ssh/id_rsa`のうちパスフレーズのない鍵を試します。パスワードではログインしません。一部のサイトが求めるワンタイムパスワードのような、キーボードインタラクティブ認証も使いません。

ホスト鍵は必ず、`known_hosts`かOpenSSHが保持するファイルと照合します。そのため、これらのファイルが知らないログインノードではステップが失敗します。先に`ssh-keyscan login.hpc.example.com >> ~/.ssh/known_hosts`のようにして追加してください。追加する前に、表示された内容を確かめてください。

各コマンドは、`ssh user@host 'squeue --json'`と同じく、ユーザーの非対話シェルの1行として実行します。ログインシェルが`PATH`を設定して初めてSlurmのコマンドが見つかる環境では、そのディレクトリを`bin_dir`に指定してください。

`key_passphrase`は結果にもアクション自身のログにも出ません。読み込む変数を[`secrets`](/ja/reference/yaml-configuration#secrets)に書けば、それ以外の箇所に現れる値も伏せられます。

## Submit

`submit`は、スクリプトを標準入力から`sbatch --parsable`に渡し、次のオプションを付けて投入します。`script`か`file`のどちらか一方を書きます。

| パラメータ | 型 | デフォルト | sbatchのオプション | 説明 |
|---|---|---|---|---|
| `script` | String | - | - | バッチスクリプト。`#!`で始めます |
| `file` | String | - | - | Probeを実行しているホストの上のバッチスクリプトのパス。作業ディレクトリからの相対パスです。そこで読み、`script`と同じように送ります |
| `name` | String | `probe` | `--job-name` | ジョブの名前 |
| `partition` | String | クラスターの既定 | `--partition` | |
| `account` | String | - | `--account` | |
| `qos` | String | - | `--qos` | |
| `time` | String or Integer | パーティションの既定 | `--time` | 制限時間。`1h30m`のような期間（分単位に切り上げます）、`1-12:00:00`のような`sbatch`の形式の時間、または`sbatch`と同じく分数で指定します |
| `nodes` | Integer or String | - | `--nodes` | `2`や`1-4`など |
| `ntasks` | Integer | - | `--ntasks` | |
| `cpus_per_task` | Integer | - | `--cpus-per-task` | |
| `mem` | String | - | `--mem` | `4G`など |
| `gres` | String | - | `--gres` | `gpu:1`など |
| `chdir` | String | ホームディレクトリ | `--chdir` | クラスターの上での、ジョブの作業ディレクトリ |
| `output` | String | `slurm-%j.out` | `--output` | クラスターの上で、ジョブが標準出力を書く場所 |
| `error` | String | `output`と同じ | `--error` | クラスターの上で、ジョブが標準エラー出力を書く場所 |
| `options` | Array | - | - | `sbatch`のそのほかのオプション。それぞれ`--exclusive`や`--constraint=a100`のように書きます |
| `keep` | Boolean | `false` | - | ジョブを実行の[後始末](#後始末)の対象から外します |

`#SBATCH --partition=long`のようにスクリプトに書いたオプションも使います。`sbatch`がコマンドラインのオプションを優先するのと同じく、ここで指定したものが優先します。

### レスポンスオブジェクト

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.job_id` | Integer | ジョブのID。`sbatch`が拒否した場合は`0` |
| `res.cluster` | String | `sbatch`がクラスターの名前を示した場合、ジョブの投入先のクラスター。なければ空です |
| `res.error` | String | `sbatch`がジョブを拒否したときのメッセージ。`sbatch: error: invalid partition specified: nope`など。なければ空です |
| `req` | Object | 実行した内容。`operation`、`host`、`port`、`user`、`bin_dir`、`script`または`file`、`keep`と、実行した`command`です。`key_passphrase`は含みません |
| `rt` | Duration | ステップにかかった時間 |
| `status` | Integer | ジョブを投入できたら`0`、そうでなければ`1` |

`sbatch`がジョブを拒否したことも結果になります。そのため、閉じているはずのパーティションやアカウントをテストで確かめられます。エラーとしてステップが失敗するのは、到達できないログインノードのように、コマンドを実行できなかった場合だけです。

## Job

`job`は、ジョブの状態を`squeue`から読みます。`slurmctld`がジョブを忘れた後は`sacct`から読みます。どちらも知らないジョブでは、ステップが失敗します。

| パラメータ | 型 | デフォルト | 説明 |
|---|---|---|---|
| `wait` | Duration | `0s` | ジョブが終わるまで待つ時間。ジョブが終わればすぐに返り、終わらなければ`wait`が過ぎた時点の状態を返します |
| `poll` | Duration | `10s` | 待つ間にジョブを確かめる間隔。`1s`以上です |
| `output` | Boolean | `false` | ジョブが始まっていれば、その標準出力を`res.stdout`に読みます |

出力は、`squeue`か`sacct`が示すパスから、コマンドを実行するホストの上で読みます。そのため、ホームディレクトリのように、ログインノードと計算ノードが共有するファイルシステムの上にある必要があります。

### レスポンスオブジェクト

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.job_id` | Integer | ジョブのID |
| `res.name` | String | ジョブの名前 |
| `res.user` | String | ジョブを実行するユーザー |
| `res.account` | String | ジョブのアカウント |
| `res.partition` | String | ジョブのパーティション |
| `res.state` | String | ジョブの状態。`PENDING`、`RUNNING`、`COMPLETED`、`FAILED`、`CANCELLED`、`TIMEOUT`、`OUT_OF_MEMORY`、`NODE_FAIL`など |
| `res.reason` | String | 待っている理由、または終わった理由。`Priority`、`Resources`、`PartitionDown`など。なければ空です |
| `res.nodes` | String | ジョブを実行しているノード。`c[1-2]`など。なければ空です |
| `res.exit_code` | Integer | スクリプトの終了コード |
| `res.signal` | Integer | ジョブを終わらせたシグナル。なければ`0` |
| `res.submit_time` | String | 投入された時刻。`2026-11-27T01:02:03Z`など |
| `res.start_time` | String | 始まった時刻。始まるまでは空です |
| `res.end_time` | String | 終わった時刻。終わるまでは空です |
| `res.wait_sec` | Integer | キューで待った秒数。待っている間はそれまでの秒数 |
| `res.elapsed_sec` | Integer | 実行した秒数。実行している間はそれまでの秒数 |
| `res.done` | Boolean | ジョブが終わったかどうか |
| `res.stdout_path` | String | クラスターの上で、ジョブが標準出力を書く場所 |
| `res.stdout` | String | `output`を指定した場合、標準出力の先頭1 MiB。始まるまでは空です |
| `res.stdout_truncated` | Boolean | `output`を指定した場合、出力が1 MiBより大きいかどうか |
| `req` | Object | `operation`、接続先、`job_id`、`wait`、`output`と`command` |
| `rt` | Duration | ステップにかかった時間 |
| `status` | Integer | ジョブが終了コード`0`で完了していれば`0`。そうでなければ、待っている間や実行している間も含めて`1` |

## Jobs

`jobs`は、`squeue`が一覧するジョブを条件で絞って一覧します。キーを書くごとに絞り込み、何も書かなければキューのすべてのジョブを一覧します。

| パラメータ | 型 | 説明 |
|---|---|---|
| `user` | String | ジョブを実行するユーザー |
| `partition` | String | ジョブのパーティション |
| `state` | String | ジョブの状態。`pending`や`RUNNING`など。なければ、`squeue`は待機中と実行中のジョブを一覧します |
| `name` | String | ジョブの名前 |
| `account` | String | ジョブのアカウント |

`res.jobs`には各ジョブが、`stdout`を除いて`job`と同じフィールドで入り、`res.count`にはその数が入ります。

## Cancel

`cancel`は、`scancel`でジョブを取り消します。`scancel`は知らないジョブにも終わったジョブにも`0`で終了するので、先にジョブを調べます。`squeue`も`sacct`も知らないジョブではステップが失敗し、終わったジョブはそのままにします。

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.job_id` | Integer | ジョブのID |
| `res.state` | String | 取り消す前のジョブの状態 |
| `res.cancelled` | Boolean | 取り消したかどうか。終わっていたジョブでは`false` |
| `res.error` | String | ほかのユーザーのジョブのように、`scancel`が拒否したときのメッセージ。なければ空です |
| `status` | Integer | `scancel`が拒否しなければ`0` |

## Partitions

`partitions`は、パーティション（指定がなければすべてのパーティション）を`scontrol show partition`から読み、そのノードを`scontrol show node`から読んだ状態ごとに数えます。`res.partitions`には各パーティションが、`res.count`にはその数が入ります。クラスターにないパーティションでは、ステップが失敗します。

| プロパティ | 型 | 説明 |
|---|---|---|
| `name` | String | パーティションの名前 |
| `state` | String | パーティションの状態。`UP`、`DOWN`、`DRAIN`、`INACTIVE`など |
| `total_nodes` | Integer | ノードの数 |
| `idle`、`mixed`、`allocated` | Integer | そのうち、空いているノード、一部が割り当てられたノード、すべてが割り当てられたノードの数 |
| `down` | Integer | ダウンしているノードの数。ドレインされているかは問いません |
| `drained` | Integer | ダウンしておらず、ドレインされた、またはドレイン中のノードの数 |
| `other` | Integer | `FUTURE`や`UNKNOWN`のような、そのほかの状態のノードの数 |

## Nodes

`nodes`は、ノードを`scontrol show node`から読みます。`res.nodes`には各ノードが、`res.count`にはその数が入ります。クラスターにないノードは含みません。

| プロパティ | 型 | 説明 |
|---|---|---|
| `name` | String | ノードの名前 |
| `state` | String | ノードの状態。`IDLE`、`MIXED`、`ALLOCATED`、`DOWN`など |
| `flags` | Array | 状態のフラグ。`DRAIN`や`NOT_RESPONDING`など |
| `partitions` | Array | ノードが属するパーティション |
| `reason` | String | ダウンまたはドレインされた理由。なければ空です |
| `cpus` | Integer | CPUの数 |
| `alloc_cpus` | Integer | そのうち割り当てられたCPUの数 |

## 後始末

実行が終わると、成功、失敗、中断のどれでも、Probeは[実行の後始末](/ja/guide/concepts/actions#実行の後始末)のとおりにアクションを呼び、その実行のステップが投入したジョブのうち、まだ終わっていないものを取り消します。そのため、待つのをやめた実行がキューにジョブを残すことはありません。`keep: true`で投入したジョブは残します。

ジョブは、投入先とあわせて一時ディレクトリの下のファイルに記録し、後始末の後にそのファイルを削除します。鍵のパスフレーズは記録しないので、`key_passphrase`で投入したジョブは、エージェントか既定の鍵でログインして取り消します。後始末でログインできなければ警告になり、警告には残ったジョブが示されます。

## ガードの下での動作

このアクションは実行のガードに従い、`action.yml`で`guard: [read-only, allow-host]`を宣言しています。そのため、`--read-only`と`--allow-host`の下でも`--allow-action`なしで実行されます。

- `--read-only`の下では、`job`、`jobs`、`partitions`、`nodes`を実行し、`submit`と`cancel`は何も実行せずに拒否します。
- `--allow-host`の下では、実行で許可されている`host`（`login.hpc.example.com`や`login.hpc.example.com:2222`など）でだけコマンドを実行します。`host`がなければ、Probeを実行しているホストに設定された`slurmctld`にコマンドが届きます。ガードではそれを確かめられないので、`host`のないステップは拒否します。

拒否されたステップは、種別`refused`で失敗します。[ガード](/ja/guide/concepts/guard)を参照してください。

## Slurmの版

アクションは、コマンドが`--json`で出力する内容を読みます。その内容はSlurmの版によって異なり、Slurm 23.02から24.11が出力するものに対して試験しています。[エンドツーエンドテスト](https://github.com/mozership/probe-slurm/blob/main/e2e/workflow.yml)では、コンテナの中のSlurm 24.11に対して実行しています。

ジョブ配列は区別しません。`job`と`cancel`はジョブのIDを受け取り、`jobs`は配列の各タスクを別々のジョブとして一覧します。

## 関連項目

- **[外部アクション](/ja/guide/concepts/actions#外部アクション)** - Probeが外部アクションを解決し、照合する仕組み
- **[SSH](/ja/reference/actions/ssh)** - SSHでのリモートサーバー上のコマンドの実行
- **[Container](/ja/reference/actions/container)** - 実行のためのコンテナの起動と、終了後の削除
