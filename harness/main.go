// harness は、テンプレートを、本番と同じエンジンと、本番と同じ k8s の展開のコードで、検査・試験するための道具。
//
// コマンド:
//   - list: テンプレートの一覧。
//   - validate [id…]: 読み込みと検査(--version で版を指定。all なら全ての版)、計画の作成(クラスターは使わない)。
//   - plan <id> [--set 名前=値]: 計画(作られる資源)を JSON で出す。
//   - render <id> [--set 名前=値]: k8s のマニフェスト(YAML)を出す(クラスターは使わない)。
//   - test [id…] [--keep]: クラスターに展開し、Ready と HTTP を確かめて、片付ける。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"launchs/shared/template"
)

// main は、サブコマンドを選んで実行し、失敗したら、終了コード 1 で終わる。
func main() {
	// サブコマンドが無ければ、使い方を出す。
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "使い方: harness <list|validate|plan|render|test> [オプション] [テンプレートの id…]")
		os.Exit(2)
	}
	// 実行して、失敗なら、メッセージを出して終了する。
	if runError := run(os.Args[1], os.Args[2:]); runError != nil {
		fmt.Fprintln(os.Stderr, "失敗:", runError)
		os.Exit(1)
	}
}

// setFlags は、--set 名前=値 を、何度でも指定できるようにする、flag の型。
type setFlags map[string]string

// String は、flag の表示用の文字列を返す。
func (flags setFlags) String() string { return fmt.Sprint(map[string]string(flags)) }

// Set は、「名前=値」を 1 つ受け取って、記録する。
//
// 引数:
//   - text: 「名前=値」の文字列。
//
// 戻り値:
//   - 形式が違うときのエラー。
func (flags setFlags) Set(text string) error {
	// 最初の「=」で、名前と値に分ける。
	name, value, found := strings.Cut(text, "=")
	if !found {
		return fmt.Errorf("--set は「名前=値」の形で指定してください: %q", text)
	}
	flags[name] = value
	return nil
}

// run は、サブコマンドを実行する。
//
// 引数:
//   - command: サブコマンドの名前。
//   - arguments: 残りの引数。
//
// 戻り値:
//   - 失敗したときのエラー。
func run(command string, arguments []string) error {
	// 共通のオプションを定義する。
	flagSet := flag.NewFlagSet(command, flag.ExitOnError)
	root := flagSet.String("root", "../templates", "テンプレートを置いたフォルダ(<id>/template.yaml を持つフォルダが並ぶところ)")
	overrides := setFlags{}
	flagSet.Var(overrides, "set", "変数の値(名前=値。何度でも指定できる)")
	kubeconfig := flagSet.String("kubeconfig", "", "kubeconfig のパス(test 用)")
	kubeContext := flagSet.String("context", "", "kubeconfig の context(test 用)")
	storageClass := flagSet.String("storage-class", "", "PVC の StorageClass(test 用。空ならクラスターの既定)")
	accessMode := flagSet.String("access-mode", "ReadWriteOnce", "PVC のアクセスモード")
	ingressNamespace := flagSet.String("ingress-namespace", "kube-system", "外からの通信を中継する namespace(k3s の Traefik は kube-system)")
	traefikAPIVersion := flagSet.String("traefik-api-version", "traefik.io/v1alpha1", "IngressRoute の apiVersion")
	version := flagSet.String("version", "", "版(x.y.z)。空なら既定の版、all なら全ての版")
	keep := flagSet.Bool("keep", false, "test の後に、namespace を消さない")
	parallel := flagSet.Int("parallel", 3, "同時に試験するテンプレートの数(test 用)")
	settle := flagSet.Duration("settle", 15*time.Second, "Ready の後に、再起動が無いかを見守る時間")
	// オプションと id が混ざっていてもよい(例: redash --version all)。id を取り出しては、残りを、またオプションとして読む。
	identifiers := []string{}
	remaining := arguments
	for {
		flagSet.Parse(remaining)
		if flagSet.NArg() == 0 {
			break
		}
		identifiers = append(identifiers, flagSet.Arg(0))
		remaining = flagSet.Args()[1:]
	}
	// テンプレートを読み込んで、検査する。
	registry, loadError := loadRegistry(*root)
	if loadError != nil {
		return loadError
	}
	// 指定された id を選ぶ。
	selected, selectError := selectTemplates(registry, identifiers, *version)
	if selectError != nil {
		return selectError
	}
	settings := runnerSettings{AccessMode: *accessMode, StorageClass: *storageClass, IngressNamespace: *ingressNamespace, TraefikAPIVersion: *traefikAPIVersion, EntryPoints: []string{"web"}}
	switch command {
	case "list":
		// 一覧を出す。
		for _, entry := range selected {
			fmt.Printf("%-14s %-8s %-12s %s  (版: %s)\n", entry.Metadata.ID, entry.Metadata.Version, entry.Metadata.Category, entry.Metadata.Name, strings.Join(entry.Metadata.Versions, ", "))
		}
		return nil
	case "validate":
		// 読み込み(検査)は通っている。計画も作れるか確かめる。
		for _, entry := range selected {
			if _, _, planError := buildPlan(entry, overrides, "harness"); planError != nil {
				return fmt.Errorf("%s: %w", entry.Metadata.ID, planError)
			}
			fmt.Printf("OK  %s %s\n", entry.Metadata.ID, entry.Metadata.Version)
		}
		return nil
	case "plan":
		// 計画を JSON で出す(パスワードなどの値も出るので、試験用の値であることに注意)。
		for _, entry := range selected {
			plan, _, planError := buildPlan(entry, overrides, "harness")
			if planError != nil {
				return planError
			}
			encoded, _ := json.MarshalIndent(plan, "", "  ")
			fmt.Println(string(encoded))
		}
		return nil
	case "render":
		// 偽のクラスターに展開して、できた資源を YAML で出す。
		for _, entry := range selected {
			plan, _, planError := buildPlan(entry, overrides, "harness")
			if planError != nil {
				return planError
			}
			clientset, dynamicClient := newFakeCluster(settings.TraefikAPIVersion)
			target, runnerError := newRunner(clientset, dynamicClient, settings)
			if runnerError != nil {
				return runnerError
			}
			namespaceName := newProjectNamespace()
			if _, applyError := target.apply(context.Background(), plan, namespaceName); applyError != nil {
				return applyError
			}
			fmt.Printf("# ===== %s %s =====\n", entry.Metadata.ID, entry.Metadata.Version)
			if dumpError := dumpManifests(context.Background(), clientset, dynamicClient, settings.TraefikAPIVersion, namespaceName, os.Stdout); dumpError != nil {
				return dumpError
			}
		}
		return nil
	case "test":
		// 本物のクラスターで試験する。
		return runTests(selected, *root, overrides, settings, *kubeconfig, *kubeContext, *keep, *settle, *parallel)
	}
	return fmt.Errorf("知らないコマンドです: %s", command)
}

// runTests は、テンプレートごとに、クラスターに展開して、確かめて、片付ける。
//
// 処理の流れ(テンプレートごと):
//  1. 計画を作り、新しい namespace(project-…)に展開する。
//  2. 全ての Deployment が Ready になり、落ち着く(再起動しない)ことを確かめる。
//  3. ingress があれば、向き先に HTTP で問い合わせて、成功とみなせる応答を確かめる。
//  4. namespace を消す(--keep なら残す)。
//
// 引数:
//   - selected: 試験するテンプレート。
//   - root: リポジトリのフォルダ(smoke.yaml を読む)。
//   - overrides: 変数の値の指定。
//   - settings: 展開の設定。
//   - kubeconfigPath: kubeconfig のパス。
//   - kubeContext: kubeconfig の context。
//   - keep: 終わっても片付けないか。
//   - settle: Ready の後に見守る時間。
//   - parallel: 同時に試験する数。
//
// 戻り値:
//   - 1 つでも失敗したら、その一覧を含むエラー。
func runTests(selected []template.Template, root string, overrides setFlags, settings runnerSettings, kubeconfigPath string, kubeContext string, keep bool, settle time.Duration, parallel int) error {
	// クラスターにつなぐ。
	clientset, dynamicClient, connectError := connectCluster(kubeconfigPath, kubeContext)
	if connectError != nil {
		return connectError
	}
	// IngressRoute の CRD が無ければ、ingress は作らない。
	if _, discoverError := clientset.Discovery().ServerResourcesForGroupVersion(settings.TraefikAPIVersion); discoverError != nil {
		fmt.Println("注意: IngressRoute の CRD が無いので、ingress の確認は飛ばします")
		settings.SkipIngress = true
	}
	target, runnerError := newRunner(clientset, dynamicClient, settings)
	if runnerError != nil {
		return runnerError
	}
	// 並列に試験する(parallel 個まで同時)。結果の表示は、混ざらないよう、排他する。
	failures := []string{}
	var lock sync.Mutex
	var group sync.WaitGroup
	slots := make(chan struct{}, max(parallel, 1))
	for _, entry := range selected {
		group.Add(1)
		slots <- struct{}{}
		go func(entry template.Template) {
			defer group.Done()
			defer func() { <-slots }()
			started := time.Now()
			// 1 つ試験する。
			testError := testOne(entry, root, overrides, target, clientset, settle, keep)
			// 結果を出す。
			lock.Lock()
			defer lock.Unlock()
			if testError != nil {
				fmt.Printf("FAIL %s %s (%s)\n%s\n", entry.Metadata.ID, entry.Metadata.Version, time.Since(started).Round(time.Second), testError)
				failures = append(failures, entry.Metadata.ID+" "+entry.Metadata.Version)
				return
			}
			fmt.Printf("PASS %s %s (%s)\n", entry.Metadata.ID, entry.Metadata.Version, time.Since(started).Round(time.Second))
		}(entry)
	}
	group.Wait()
	if len(failures) > 0 {
		return fmt.Errorf("失敗したテンプレート: %s", strings.Join(failures, ", "))
	}
	return nil
}

// testOne は、1 つのテンプレートを試験する(runTests の 1 回分)。
//
// 引数: runTests と同じ(1 つのテンプレートと、組み立て済みの runner、clientset)。
//
// 戻り値:
//   - 失敗したときのエラー。
func testOne(entry template.Template, root string, overrides setFlags, target *runner, clientset kubernetes.Interface, settle time.Duration, keep bool) error {
	requestContext := context.Background()
	// 計画を作る。
	plan, _, planError := buildPlan(entry, overrides, "harness")
	if planError != nil {
		return planError
	}
	namespaceName := newProjectNamespace()
	// 終わったら、namespace を消す。
	if !keep {
		defer clientset.CoreV1().Namespaces().Delete(requestContext, namespaceName, metav1.DeleteOptions{})
	}
	fmt.Printf("---- %s %s: namespace %s\n", entry.Metadata.ID, entry.Metadata.Version, namespaceName)
	// 展開する。
	routes, applyError := target.apply(requestContext, plan, namespaceName)
	if applyError != nil {
		return applyError
	}
	smoke, smokeError := loadSmokeSettings(root, entry.Metadata.ID, entry.Metadata.Version)
	if smokeError != nil {
		return smokeError
	}
	// Ready と、落ち着くのを待つ。
	if waitError := waitDeploymentsReady(requestContext, target, clientset, namespaceName, time.Duration(smoke.TimeoutSeconds)*time.Second, settle); waitError != nil {
		return waitError
	}
	// ingress の向き先に、HTTP で問い合わせる。
	for _, route := range routes {
		if httpError := checkHTTP(requestContext, clientset, namespaceName, route, smoke); httpError != nil {
			return httpError
		}
	}
	return nil
}
