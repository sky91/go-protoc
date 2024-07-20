package goprotoc

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"github.com/pkg/errors"
	"github.com/samber/lo"
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
	"text/template"
	"time"
)

type logger interface {
	Infof(format string, args ...any)
	Errorf(format string, args ...any)
}

type Generator struct {
	ProtoDir           string
	ProtocDownloadUrl  string
	ProtocVer          string
	ProtocGenGoGrpcVer string
	CleanDir           string
	CustomProtocOpts   []string
	DisableJetBrains   bool
	Logger             logger

	getProtocDownloadUrl func() (string, error)
	getProtocDistPath    func() (string, error)
}

func (thisP *Generator) Init() error {
	if thisP.Logger == nil {
		thisP.Logger = internal.FuncLogger(log.Printf)
	}
	thisP.getProtocDownloadUrl = sync.OnceValues(thisP.doGetProtocDownloadUrl)
	thisP.getProtocDistPath = sync.OnceValues(thisP.doGetProtocDistPath)
	return nil
}

func (thisP *Generator) Run(ctx context.Context) error {
	genFilePkg, cmd, err := internal.GoListPkg(os.Getenv("GOFILE"), []string{"generate"})
	if err != nil {
		return errors.Wrapf(err, "GoListPkg() error: cmd=[%+v]", cmd)
	}
	if genFilePkg.Error != nil {
		return fmt.Errorf("GoListPkg() error: cmd=[%+v], pbGoPkg.Error=[%+v]", cmd, genFilePkg.Error)
	}
	thisP.Logger.Infof("GoListPkg() ok: cmd=[%+v]", cmd)

	genPkg, cmd, err := internal.GoListPkg(genFilePkg.Dir, []string{"generate"})
	if err != nil {
		return errors.Wrapf(err, "GoListPkg() error: cmd=[%+v]", cmd)
	}
	if genFilePkg.Error != nil {
		return fmt.Errorf("GoListPkg() error: cmd=[%+v], pbGoPkg.Error=[%+v]", cmd, genFilePkg.Error)
	}
	thisP.Logger.Infof("GoListPkg() ok: cmd=[%+v]", cmd)

	// protoc
	if err = thisP.prepareProtoc(ctx); err != nil {
		return errors.Wrapf(err, "prepareProtoc() error")
	}

	// protoc-gen-go
	protocGenGoInstallDir, err := thisP.prepareProtocGenGo()
	if err != nil {
		return errors.Wrapf(err, "prepareProtocGenGo() error")
	}

	// protoc-gen-go-grpc
	protocGenGoGrpcInstallDir, err := thisP.prepareProtocGenGoGrpc()
	if err != nil {
		return errors.Wrapf(err, "prepareProtocGenGoGrpc() error")
	}

	protocOpts := bytes.Buffer{}

	// proto_path
	protoPaths, err := listImportPathDir(genFilePkg.Imports)
	if err != nil {
		return errors.Wrapf(err, "listImportPathDir() error")
	}
	protocDistPath, err := thisP.getProtocDistPath()
	if err != nil {
		return errors.Wrapf(err, "getProtocDistPath() error")
	}
	for _, protoPath := range append(protoPaths, thisP.getProtoDirAbs(genFilePkg.Dir), filepath.Join(protocDistPath, "include")) {
		_, _ = protocOpts.WriteString(fmt.Sprintf("--proto_path=%s\n", protoPath))
	}

	// JetBrains plugin ProtoEditor
	protoEditorGroup := errgroup.Group{}
	protoEditorGroup.Go(func() error { return configProtoEditor(genPkg, protoPaths) })
	defer func() {
		if err := protoEditorGroup.Wait(); err != nil {
			thisP.Logger.Errorf("configProtoEditor error: %+v", err)
		}
	}()
	thisP.Logger.Infof("ProtoEditor config ok")

	// protoc go
	goExe, err := internal.GoEnv("GOEXE")
	if err != nil {
		return errors.Wrapf(err, "GoEnv() error")
	}
	_, _ = protocOpts.WriteString(fmt.Sprintf("--go_out=%s\n", genPkg.Module.Dir))
	_, _ = protocOpts.WriteString(fmt.Sprintf("--go_opt=module=%s\n", genPkg.Module.Path))
	_, _ = protocOpts.WriteString(fmt.Sprintf("--plugin=protoc-gen-go=%s\n", filepath.Join(protocGenGoInstallDir, "protoc-gen-go"+goExe)))

	// protoc go grpc
	if protocGenGoGrpcInstallDir != "" {
		_, _ = protocOpts.WriteString(fmt.Sprintf("--go-grpc_out=%s\n", genPkg.Module.Dir))
		_, _ = protocOpts.WriteString(fmt.Sprintf("--go-grpc_opt=module=%s\n", genPkg.Module.Path))
		_, _ = protocOpts.WriteString(fmt.Sprintf("--plugin=protoc-gen-go-grpc=%s\n", filepath.Join(protocGenGoGrpcInstallDir, "protoc-gen-go-grpc"+goExe)))
	}

	// proto file
	if err = filepath.WalkDir(thisP.getProtoDirAbs(genFilePkg.Dir), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrap(err, "WalkDirFunc error")
		}
		if d.IsDir() || !strings.HasSuffix(path, ".proto") {
			return nil
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("filepath.Abs() error: [%w]", err)
		}
		_, _ = protocOpts.WriteString(absPath + "\n")
		return nil
	}); err != nil {
		return errors.Wrapf(err, "filepath.WalkDir() error")
	}

	// proto gen file
	protoGenFile := thisP.getProtoGenFilePath(genPkg)
	if err = os.MkdirAll(filepath.Dir(protoGenFile), 0755); err != nil {
		return errors.Wrapf(err, "os.MkdirAll() error")
	}
	if err = os.WriteFile(protoGenFile, protocOpts.Bytes(), 0666); err != nil {
		return errors.Wrapf(err, "os.WriteFile() error")
	}
	thisP.Logger.Infof("write proto gen file ok: [%s]", protoGenFile)

	// clean dir
	for _, dir := range strings.Split(thisP.getCleanDir(), ",") {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(genFilePkg.Dir, dir)
		}

		thisP.Logger.Infof("clean dir: [%s]", dir)
		if err = os.RemoveAll(dir); err != nil {
			return errors.Wrapf(err, "os.RemoveAll() error")
		}
	}

	cmd = exec.Command(filepath.Join(protocDistPath, "bin", "protoc"), "@"+protoGenFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	thisP.Logger.Infof("cmd begin: cmd=[%+v]", cmd)
	if err = cmd.Run(); err != nil {
		return errors.Wrapf(err, "cmd.Run() error: cmd=[%+v]", cmd)
	} else {
		thisP.Logger.Infof("cmd ok: cmd=[%+v]", cmd)
	}
	return nil
}

func (thisP *Generator) prepareProtoc(ctx context.Context) error {
	protocDistZipFilepath, err := thisP.getProtocDistZipFilePath()
	if err != nil {
		return errors.Wrapf(err, "getProtocDistZipFilePath() error")
	}
	if err = os.MkdirAll(filepath.Dir(protocDistZipFilepath), 0755); err != nil {
		return errors.Wrapf(err, "os.MkdirAll() error")
	}
	thisP.Logger.Infof("protocDistZipFilepath: [%s]", protocDistZipFilepath)

	zipFileBytes, err := os.ReadFile(protocDistZipFilepath)
	if err == nil && len(zipFileBytes) < 1024*1024 ||
		err != nil && os.IsNotExist(err) {
		downloadUrl, err := thisP.getProtocDownloadUrl()
		if err != nil {
			return errors.Wrapf(err, "getProtocDownloadUrl() error")
		}
		thisP.Logger.Infof("try download protoc from: [%s]", downloadUrl)
		downloadCtx, downloadCtxCancel := context.WithTimeout(ctx, 30*time.Second)
		defer downloadCtxCancel()
		if err = downloadProtocZip(downloadCtx, downloadUrl, protocDistZipFilepath); err != nil {
			return errors.Wrapf(err, "downloadProtocZip() error")
		}
		if zipFileBytes, err = os.ReadFile(protocDistZipFilepath); err != nil {
			return errors.Wrapf(err, "os.ReadFile() error")
		}
	} else if err != nil {
		return errors.Wrapf(err, "os.ReadFile() error")
	}

	zipReader, err := zip.NewReader(bytes.NewReader(zipFileBytes), int64(len(zipFileBytes)))
	if err != nil {
		return errors.Wrapf(err, "zip.NewReader() error")
	}
	protocDistPath, err := thisP.getProtocDistPath()
	if err != nil {
		return errors.Wrapf(err, "getProtocDistPath() error")
	}
	if err = os.RemoveAll(protocDistPath); err != nil {
		return errors.Wrapf(err, "os.RemoveAll() error")
	}
	if err = internal.Unzip(zipReader, protocDistPath); err != nil {
		return fmt.Errorf("unzip() error: [%w]", err)
	}
	thisP.Logger.Infof("unzip() ok: [%s]", protocDistPath)
	return nil
}

func (thisP *Generator) prepareProtocGenGo() (installDir string, err error) {
	ProtocGenGoPkg, cmd, err := internal.GoListPkg(pkgNameProtocGenGo, nil)
	if err != nil {
		return "", errors.Wrapf(err, "GoListPkg() error: cmd=[%+v]", cmd)
	}
	if ProtocGenGoPkg.Error != nil {
		return "", fmt.Errorf("GoListPkg() error: cmd=[%+v], err=[%+v]", cmd, ProtocGenGoPkg.Error)
	}
	thisP.Logger.Infof("GoListPkg() ok: cmd=[%+v]", cmd)
	thisP.Logger.Infof("pkg found: [%s@%s]", pkgNameProtocGenGo, ProtocGenGoPkg.Module.Version)

	installDir = thisP.getProtocGenGoPath(ProtocGenGoPkg.Module.Version)
	if err = os.MkdirAll(installDir, 0755); err != nil {
		return "", errors.Wrap(err, "os.MkdirAll() error")
	}
	if cmd, err = internal.GoInstall(pkgNameProtocGenGo, installDir); err != nil {
		return "", errors.Wrapf(err, "GoInstall() error: cmd=[%+v]", cmd)
	}
	thisP.Logger.Infof("GoInstall() ok: cmd=[%+v], dest=[%s]", cmd, installDir)
	return installDir, nil
}

func (thisP *Generator) prepareProtocGenGoGrpc() (installDir string, err error) {
	grpcPkg, cmd, err := internal.GoListPkg(pkgNameGrpc, nil)
	if err != nil {
		return "", errors.Wrapf(err, "GoListPkg() error: cmd=[%+v]", cmd)
	}
	if grpcPkg.Error != nil {
		thisP.Logger.Infof("pkg [%s] not found, will not generate grpc, cmd=[%+v]", pkgNameGrpc, cmd)
		return "", nil
	}
	thisP.Logger.Infof("pkg found: [%s@%s]", pkgNameGrpc, grpcPkg.Module.Version)
	installDir = thisP.getProtocGenGoGrpcPath(thisP.getProtocGenGoGrpcVer())
	if err = os.MkdirAll(installDir, 0755); err != nil {
		return "", errors.Wrapf(err, "os.MkdirAll() error")
	}
	if cmd, err = internal.GoInstall(pkgNameProtocGenGoGrpc+"@v"+thisP.getProtocGenGoGrpcVer(), installDir); err != nil {
		return "", fmt.Errorf("GoInstall() error: cmd=[%+v], err=[%w]", cmd, err)
	}
	thisP.Logger.Infof("GoInstall() ok: cmd=[%+v], dest=[%s]", cmd, installDir)
	return installDir, nil
}

func (thisP *Generator) doGetProtocDownloadUrl() (string, error) {
	downloadUrl := thisP.ProtocDownloadUrl
	if downloadUrl == "" {
		downloadUrl = defaultProtocDownloadUrl
	}
	tmpl, err := template.New("protocDownloadUrl").Parse(downloadUrl)
	if err != nil {
		return "", errors.Wrapf(err, "template.New().Parse() error")
	}

	var buf bytes.Buffer
	if err = tmpl.Execute(&buf, struct {
		Version string
		OsArch  string
	}{Version: thisP.getProtocVer(), OsArch: osArch}); err != nil {
		return "", errors.Wrapf(err, "tmpl.Execute() error")
	}
	return buf.String(), nil
}

func (thisP *Generator) doGetProtocDistPath() (string, error) {
	downloadUrl, err := thisP.getProtocDownloadUrl()
	if err != nil {
		return "", errors.Wrapf(err, "getProtocDownloadUrl() error")
	}
	urlHash := sha256.Sum256([]byte(downloadUrl))
	urlHashBase64 := base64.RawURLEncoding.EncodeToString(urlHash[:])
	return filepath.Join(protocDistDir, urlHashBase64), nil
}

func (thisP *Generator) getProtocDistZipFilePath() (string, error) {
	distPath, err := thisP.getProtocDistPath()
	if err != nil {
		return "", errors.Wrapf(err, "getProtocDistPath() error")
	}
	return distPath + ".zip", nil
}

func (thisP *Generator) getProtocVer() string {
	if thisP.ProtocVer != "" {
		return thisP.ProtocVer
	}
	return defaultProtocVer
}

func (thisP *Generator) getProtocGenGoGrpcVer() string {
	if thisP.ProtocGenGoGrpcVer != "" {
		return thisP.ProtocGenGoGrpcVer
	}
	return defaultProtocGenGoGrpcVer
}

func (thisP *Generator) getProtocGenGoPath(protocGenGoVer string) string {
	return filepath.Join(protocGenGoDir, protocGenGoVer)
}

func (thisP *Generator) getProtocGenGoGrpcPath(protocGenGoGrpcVer string) string {
	return filepath.Join(protocGenGoGrpcDir, protocGenGoGrpcVer)
}

func (thisP *Generator) getProtoDir() string {
	if thisP.ProtoDir != "" {
		return thisP.ProtoDir
	}
	return defaultProtoDir
}

func (thisP *Generator) getProtoDirAbs(current string) string {
	protoDir := thisP.getProtoDir()
	if filepath.IsAbs(protoDir) {
		return protoDir
	}
	return filepath.Join(current, protoDir)
}

func (thisP *Generator) getProtoGenFilePath(genPkg *internal.PackagePublic) string {
	return filepath.Join(os.TempDir(), ".go-protoc", regexp.MustCompile(`\W+`).ReplaceAllString(genPkg.ImportPath, "_"), "proto_gen.txt")
}

func (thisP *Generator) getCleanDir() string {
	if thisP.CleanDir != "" {
		return thisP.CleanDir
	}
	return defaultCleanDir
}

func getProtocOsArch() (string, error) {
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
		switch goarch {
		case "amd64":
			return "osx-x86_64", nil
		case "arm64":
			return "osx-aarch_64", nil
		default:
			return "osx-universal_binary", nil
		}
	case "linux":
		switch goarch {
		case "386":
			return "linux-x86_32", nil
		case "amd64":
			return "linux-x86_64", nil
		case "arm64":
			return "linux-aarch_64", nil
		}
	case "windows":
		switch goarch {
		case "386":
			return "win32", nil
		case "amd64":
			return "win64", nil
		}
	}
	return "", fmt.Errorf("not support os:[%s] and arch:[%s]", goos, goarch)
}

func downloadProtocZip(ctx context.Context, zipUrl string, destFile string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, zipUrl, nil)
	if err != nil {
		return errors.Wrapf(err, "http.NewRequestWithContext() error")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Wrapf(err, "http.DefaultClient.Do() error")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("http.DefaultClient.Do() error: statusCode=[%d]", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrapf(err, "io.ReadAll() error")
	}

	if err = os.WriteFile(destFile, bodyBytes, 0666); err != nil {
		return errors.Wrapf(err, "os.WriteFile() error")
	}
	return nil
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
		switch goarch {
		case "amd64":
			return fmt.Sprintf("protoc-%s-osx-x86_64.zip", ver)
		case "arm64":
			return fmt.Sprintf("protoc-%s-aarch_64.zip", ver)
		default:
			return fmt.Sprintf("protoc-%s-osx-universal_binary.zip", ver)
		}
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

func configProtoEditor(genPkg *internal.PackagePublic, protoPaths []string) error {
	protoEditor, err := internal.FindJetBrainsRootAndOpen(genPkg.Dir)
	if err != nil {
		return nil
	}
	protoEditor.ConfigProtoPath(genPkg.ImportPath, protoPaths)
	if err = protoEditor.Save(); err != nil {
		return fmt.Errorf("func protoEditor.Save() error: [%w]", err)
	}
	return nil
}

const (
	defaultProtocDownloadUrl  = `https://github.com/protocolbuffers/protobuf/releases/download/v{{.Version}}/protoc-{{.Version}}-{{.OsArch}}.zip`
	defaultProtocVer          = "27.2"
	defaultProtocGenGoGrpcVer = "1.4.0"
	defaultProtoDir           = "proto"
	defaultCleanDir           = "proto_gen_go"

	pkgNameProtocGenGo     = "google.golang.org/protobuf/cmd/protoc-gen-go"
	pkgNameGrpc            = "google.golang.org/grpc"
	pkgNameProtocGenGoGrpc = "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
)

var (
	osArch             = lo.Must(getProtocOsArch())
	rootDir            = filepath.Join(lo.Must(os.UserCacheDir()), ".go_protoc")
	protocDistDir      = filepath.Join(rootDir, "protoc")
	protocGenGoDir     = filepath.Join(rootDir, "protoc-gen-go")
	protocGenGoGrpcDir = filepath.Join(rootDir, "protoc-gen-go-grpc")
)
