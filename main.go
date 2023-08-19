package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"github.com/sky91/go-protoc/internal"
	"golang.org/x/sync/errgroup"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

const (
	flagName_GenFile               = "gen-file"
	flagName_ProtoDir              = "proto-dir"
	flagName_ProtocDownloadUrl     = "protoc-dl-url"
	flagName_ProtocVer             = "protoc-ver"
	flagName_DescOutFile           = "desc-out-file"
	flagName_DescIncludeImports    = "desc-include-imports"
	flagName_DescIncludeSourceInfo = "desc-include-source-info"
	flagName_CleanDir              = "clean-dir"

	pkgName_ProtocGenGo     = "google.golang.org/protobuf/cmd/protoc-gen-go"
	pkgName_ProtocGenGoGrpc = "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
)

func main() {
	flagGenFile := flag.String(flagName_GenFile, os.Getenv("GOFILE"), ".go file which contains dependencies import")
	flagProtoDir := flag.String(flagName_ProtoDir, "proto", "dir path in which .proto files reside")
	flagProtocDownloadUrl := flag.String(flagName_ProtocDownloadUrl, "", "protoc download url, e.g. [https://github.com/protocolbuffers/protobuf/releases/download/v3.15.6/protoc-3.15.6-win64.zip]")
	flagProtocVer := flag.String(flagName_ProtocVer, "24.1", "version of protoc")
	flagDescOutFile := flag.String(flagName_DescOutFile, "", "file that pb-marshalled FileDescriptorSet written to, see also: protoc --descriptor_set_out=FILE")
	flagDescIncludeImports := flag.Bool(flagName_DescIncludeImports, false, "see also: protoc --include_imports")
	flagDescIncludeSourceInfo := flag.Bool(flagName_DescIncludeSourceInfo, false, "see also: protoc --include_source_info")
	flagCleanDir := flag.String(flagName_CleanDir, "proto_gen_go", "dir relative to current to clean before generate")
	flag.Parse()

	log.Printf("[%v]=[%v]", flagName_GenFile, *flagGenFile)
	log.Printf("[%v]=[%v]", flagName_ProtoDir, *flagProtoDir)
	log.Printf("[%v]=[%v]", flagName_ProtocDownloadUrl, *flagProtocDownloadUrl)
	log.Printf("[%v]=[%v]", flagName_ProtocVer, *flagProtocVer)
	log.Printf("[%v]=[%v]", flagName_DescOutFile, *flagDescOutFile)
	log.Printf("[%v]=[%v]", flagName_CleanDir, *flagCleanDir)

	genFilePkg, cmd, err := internal.GoListPkg(*flagGenFile, []string{"generate"})
	if err != nil {
		log.Fatalf("GoListPkg() error: cmd=[%+v], err=[%+v]", cmd, err)
	}
	if genFilePkg.Error != nil {
		log.Fatalf("GoListPkg() error: cmd=[%+v], pbGoPkg.Error=[%+v]", cmd, genFilePkg.Error)
	}
	log.Printf("GoListPkg() ok: cmd=[%+v]", cmd)

	genPkg, cmd, err := internal.GoListPkg(genFilePkg.Dir, []string{"generate"})
	if err != nil {
		log.Fatalf("GoListPkg() error: cmd=[%+v], err=[%+v]", cmd, err)
	}
	if genFilePkg.Error != nil {
		log.Fatalf("GoListPkg() error: cmd=[%+v], pbGoPkg.Error=[%+v]", cmd, genFilePkg.Error)
	}
	log.Printf("GoListPkg() ok: cmd=[%+v]", cmd)

	workRootDir := filepath.Join(os.TempDir(), "go-protoc")
	projWorkRootDir := filepath.Join(workRootDir, regexp.MustCompile(`\W+`).ReplaceAllString(genPkg.ImportPath, "_"))
	if err = os.RemoveAll(projWorkRootDir); err != nil {
		log.Fatalf("func os.RemoveAll() error: [%+v]", err)
	}
	if err = os.MkdirAll(projWorkRootDir, 0777); err != nil {
		log.Fatalf("os.MkdirAll() error: [%+v]", err)
	}

	asyncGroup := errgroup.Group{}
	asyncGroup.Go(func() error {
		protocGenGoPkg, cmd, err := internal.GoListPkg(pkgName_ProtocGenGo, nil)
		if err != nil {
			return fmt.Errorf("GoListPkg() error: cmd=[%+v], err=[%w]", cmd, err)
		}
		if protocGenGoPkg.Error != nil {
			return fmt.Errorf("GoListPkg() error: cmd=[%+v], err=[%+v]", cmd, protocGenGoPkg.Error)
		}
		log.Printf("GoListPkg() ok: cmd=[%+v]", cmd)
		log.Printf("pkg found: [%s@%s]", pkgName_ProtocGenGo, protocGenGoPkg.Module.Version)
		if cmd, err = internal.GoInstall(pkgName_ProtocGenGo, projWorkRootDir); err != nil {
			return fmt.Errorf("GoInstall() error: cmd=[%+v], err=[%w]", cmd, err)
		}
		log.Printf("GoInstall() ok: cmd=[%+v], dest=[%s]", cmd, projWorkRootDir)
		return nil
	})

	protocGenGoGrpcPkg, cmd, err := internal.GoListPkg(pkgName_ProtocGenGoGrpc, nil)
	if err != nil {
		log.Fatalf("GoListPkg() error: cmd=[%+v], err=[%+v]", cmd, err)
	}
	asyncGroup.Go(func() error {
		if protocGenGoGrpcPkg.Error != nil {
			log.Printf("pkg [%s] not found, will not generate grpc", pkgName_ProtocGenGoGrpc)
			return nil
		}
		log.Printf("pkg found: [%s@%s]", pkgName_ProtocGenGoGrpc, protocGenGoGrpcPkg.Module.Version)
		if cmd, err = internal.GoInstall(pkgName_ProtocGenGoGrpc, projWorkRootDir); err != nil {
			return fmt.Errorf("GoInstall() error: cmd=[%+v], err=[%w]", cmd, err)
		}
		log.Printf("GoInstall() ok: cmd=[%+v], dest=[%s]", cmd, projWorkRootDir)
		return nil
	})

	protocZipName := getProtocFileName(*flagProtocVer)
	projProtocDir := filepath.Join(projWorkRootDir, "protoc")
	if err = os.RemoveAll(projProtocDir); err != nil {
		log.Fatalf("func os.RemoveAll() error: [%+v]", err)
	}
	if err = os.MkdirAll(projProtocDir, 0777); err != nil {
		log.Fatalf("func os.MkdirAll(): [%+v]", err)
	}

	protocZipUrls := make([]string, 0, 5)
	if len(*flagProtocDownloadUrl) > 0 {
		protocZipUrls = append(protocZipUrls, *flagProtocDownloadUrl)
	}
	protocZipUrls = append(protocZipUrls,
		fmt.Sprintf("https://github.com/protocolbuffers/protobuf/releases/download/v%s/%s", *flagProtocVer, protocZipName))

	asyncGroup.Go(func() error { return downloadProtoc(protocZipUrls, projProtocDir) })

	protoGenFileName := filepath.Join(projWorkRootDir, "proto_gen.txt")
	protoGenFile, err := os.Create(protoGenFileName)
	if err != nil {
		log.Fatalf("os.Create() error: [%+v]", err)
	}
	protoGenFileCloseOnce := sync.Once{}
	defer protoGenFileCloseOnce.Do(func() { _ = protoGenFile.Close() })

	if !filepath.IsAbs(*flagProtoDir) {
		*flagProtoDir = filepath.Join(genFilePkg.Dir, *flagProtoDir)
	}
	protoPaths, err := listImportPathDir(genFilePkg.Imports)
	if err != nil {
		log.Fatalf("listImportPathDir() error: %+v", err)
	}
	protoPaths = append(protoPaths, *flagProtoDir, filepath.Join(projProtocDir, "include"))

	for _, protoPath := range protoPaths {
		if _, err = fmt.Fprintf(protoGenFile, "--proto_path=%s\n", protoPath); err != nil {
			log.Fatalf("func fmt.Fprintf() error: [%+v]", err)
		}
	}

	// protoc flag about writing FileDescriptorSet: --descriptor_set_out=FILE
	if *flagDescOutFile != "" {
		if _, err := fmt.Fprintf(protoGenFile, "--descriptor_set_out=%s\n", *flagDescOutFile); err != nil {
			log.Fatalf("fmt.Fprintf() error: [%+v]", err)
		}
		if *flagDescIncludeImports {
			if _, err := fmt.Fprintln(protoGenFile, "--include_imports"); err != nil {
				log.Fatalf("fmt.Fprintln() error: [%+v]", err)
			}
		}
		if *flagDescIncludeSourceInfo {
			if _, err := fmt.Fprintln(protoGenFile, "--include_source_info"); err != nil {
				log.Fatalf("fmt.Fprintln() error: [%+v]", err)
			}
		}
	}

	// JetBrains plugin ProtoEditor
	protoEditorGroup := errgroup.Group{}
	protoEditorGroup.Go(func() error { return configProtoEditor(genPkg, protoPaths) })
	defer func() {
		if err := protoEditorGroup.Wait(); err != nil {
			log.Printf("configProtoEditor error: %+v", err)
		}
	}()

	// --go_out
	if _, err = fmt.Fprintf(protoGenFile, "--go_out=%s\n", genPkg.Module.Dir); err != nil {
		log.Fatalf("fmt.Fprintf() error: [%+v]", err)
	}
	if _, err = fmt.Fprintf(protoGenFile, "--go_opt=module=%s\n", genPkg.Module.Path); err != nil {
		log.Fatalf("fmt.Fprintf() error: [%+v]", err)
	}

	if protocGenGoGrpcPkg.Error == nil {
		if _, err = fmt.Fprintf(protoGenFile, "--go-grpc_out=%s\n", genPkg.Module.Dir); err != nil {
			log.Fatalf("fmt.Fprintf() error: [%+v]", err)
		}
		if _, err = fmt.Fprintf(protoGenFile, "--go-grpc_opt=module=%s\n", genPkg.Module.Path); err != nil {
			log.Fatalf("fmt.Fprintf() error: [%+v]", err)
		}
	}

	if err = filepath.WalkDir(*flagProtoDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".proto") {
			return nil
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("filepath.Abs() error: [%w]", err)
		}
		if _, err = protoGenFile.WriteString(absPath + "\n"); err != nil {
			return fmt.Errorf("protoGenFile.WriteString() error: [%w]", err)
		}
		return nil
	}); err != nil {
		log.Fatalf("filepath.WalkDir() error: [%+v]", err)
	}

	protoGenFileCloseOnce.Do(func() {
		if err = protoGenFile.Close(); err != nil {
			log.Fatalf("protoGenFile.Close() error: [%+v]", err)
		}
	})

	for _, dir := range strings.Split(*flagCleanDir, ",") {
		dir = strings.TrimSpace(dir)
		if len(dir) == 0 {
			continue
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(genFilePkg.Dir, dir)
		}

		log.Printf("clean dir: [%s]", dir)
		if err = os.RemoveAll(dir); err != nil {
			log.Fatalf("os.RemoveAll() error: [%+v]", err)
		}
	}

	envs := make([]string, 0, 64)
	for _, e := range os.Environ() {
		if len(e) >= 5 && strings.EqualFold(e[:5], "path=") {
			envs = append(envs, e[:5]+projWorkRootDir+string(os.PathListSeparator)+e[5:])
		} else {
			envs = append(envs, e)
		}
	}

	if err = asyncGroup.Wait(); err != nil {
		log.Fatalf("downloadProtoc() error: %+v", err)
	}
	cmd = exec.Command(filepath.Join(projProtocDir, "bin", "protoc"), "@"+protoGenFileName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = envs
	log.Printf("cmd begin: cmd=[%+v]", cmd)
	if err = cmd.Run(); err != nil {
		log.Fatalf("cmd error: cmd=[%+v], err=[%+v]", cmd, err)
	} else {
		log.Printf("cmd ok: cmd=[%+v]", cmd)
	}
}

func getProtocFileName(ver string) string {
	goos, ok := os.LookupEnv("GOOS")
	if !ok {
		goos = runtime.GOOS
	}
	goarch, ok := os.LookupEnv("GOARCH")
	if !ok {
		goarch = runtime.GOARCH
	}
	switch goos {
	case "darwin":
		return fmt.Sprintf("protoc-%s-osx-x86_64.zip", ver)
	case "linux":
		switch goarch {
		case "386":
			return fmt.Sprintf("protoc-%s-linux-x86_32.zip", ver)
		case "amd64":
			return fmt.Sprintf("protoc-%s-linux-x86_64.zip", ver)
		}
	case "windows":
		switch goarch {
		case "386":
			return fmt.Sprintf("protoc-%s-win32.zip", ver)
		case "amd64":
			return fmt.Sprintf("protoc-%s-win64.zip", ver)
		}
	}
	panic(fmt.Sprintf("not support os:[%s] and arch:[%s]", goos, goarch))
}

func downloadProtoc(urls []string, destDir string) error {
	var bodyBytes []byte
	for _, u := range urls {
		log.Printf("try download protoc from: [%s]", u)
		resp, err := http.Get(u)
		if err != nil {
			log.Printf("http.Get() error: [%+v]", err)
			continue
		}
		bodyBytes, err = io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("io.ReadAll() error: [%+v]", err)
			_ = resp.Body.Close()
			continue
		}
		_ = resp.Body.Close()
		break
	}
	if len(bodyBytes) == 0 {
		return errors.New("download protoc fail")
	}

	reader, err := zip.NewReader(bytes.NewReader(bodyBytes), int64(len(bodyBytes)))
	if err != nil {
		return fmt.Errorf("zip.NewReader() error: [%w]", err)
	}

	if err = internal.Unzip(reader, destDir); err != nil {
		_ = os.RemoveAll(destDir)
		return fmt.Errorf("unzip() error: [%w]", err)
	}
	log.Printf("unzip protoc release into [%s] ok", destDir)
	return nil
}

func configProtoEditor(genPkg *internal.PackagePublic, protoPaths []string) error {
	protoEditor, err := internal.FindJetBrainsRootAndOpen(genPkg.Dir)
	if err != nil {
		log.Printf("ProtoEditor config not found: [%+v]", err)
		return nil
	}
	protoEditor.ConfigProtoPath(genPkg.ImportPath, protoPaths)
	if err = protoEditor.Save(); err != nil {
		return fmt.Errorf("func protoEditor.Save() error: [%w]", err)
	}
	log.Printf("ProtoEditor config ok")
	return nil
}

func listImportPathDir(importPaths []string) ([]string, error) {
	dirs := make([]string, 0, len(importPaths))
	mtx := sync.Mutex{}
	group := errgroup.Group{}
	for _, importPath := range importPaths {
		importPath := importPath
		group.Go(func() error {
			if len(importPath) == 0 {
				return nil
			}
			importPkgInfo, cmd, err := internal.GoListPkg(importPath, []string{"generate"})
			if err != nil {
				return fmt.Errorf("GoListPkg() error: cmd=[%+v], err=[%w]", cmd, err)
			}
			if len(importPkgInfo.Dir) == 0 {
				if importPkgInfo.Error != nil {
					return fmt.Errorf("GoListPkg() error: cmd=[%+v], importPkgInfo.Error=[%+v]", cmd, importPkgInfo.Error)
				}
				return fmt.Errorf("GoListPkg() error, cannot find pkg dir: cmd=[%+v]", cmd)
			}
			log.Printf("GoListPkg() ok: cmd=[%+v]", cmd)
			mtx.Lock()
			dirs = append(dirs, importPkgInfo.Dir)
			mtx.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return dirs, nil
}
