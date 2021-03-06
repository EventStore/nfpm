package apk

import (
	"archive/tar"
	"bytes"
	"crypto/sha1" // nolint:gosec
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/goreleaser/nfpm"
	"github.com/goreleaser/nfpm/internal/sign"

	"github.com/stretchr/testify/assert"
)

// nolint: gochecknoglobals
var update = flag.Bool("update", false, "update apk .golden files")

func exampleInfo() *nfpm.Info {
	return nfpm.WithDefaults(&nfpm.Info{
		Name:        "foo",
		Arch:        "amd64",
		Description: "Foo does things",
		Priority:    "extra",
		Maintainer:  "Carlos A Becker <pkg@carlosbecker.com>",
		Version:     "v1.0.0",
		Release:     "r1",
		Section:     "default",
		Homepage:    "http://carlosbecker.com",
		Vendor:      "nope",
		Overridables: nfpm.Overridables{
			Depends: []string{
				"bash",
				"foo",
			},
			Recommends: []string{
				"git",
				"bar",
			},
			Suggests: []string{
				"bash",
				"lala",
			},
			Replaces: []string{
				"svn",
				"subversion",
			},
			Provides: []string{
				"bzr",
				"zzz",
			},
			Conflicts: []string{
				"zsh",
				"foobarsh",
			},
			Files: map[string]string{
				"../testdata/fake":          "/usr/local/bin/fake",
				"../testdata/whatever.conf": "/usr/share/doc/fake/fake.txt",
			},
			ConfigFiles: map[string]string{
				"../testdata/whatever.conf": "/etc/fake/fake.conf",
			},
			EmptyFolders: []string{
				"/var/log/whatever",
				"/usr/share/whatever",
			},
		},
	})
}

func TestArchToAlpine(t *testing.T) {
	verifyArch(t, "", "")
	verifyArch(t, "abc", "abc")
	verifyArch(t, "386", "x86")
	verifyArch(t, "amd64", "x86_64")
	verifyArch(t, "arm", "armhf")
	verifyArch(t, "arm6", "armhf")
	verifyArch(t, "arm7", "armhf")
	verifyArch(t, "arm64", "aarch64")
}

func verifyArch(t *testing.T, nfpmArch, expectedArch string) {
	info := exampleInfo()
	info.Arch = nfpmArch

	assert.NoError(t, Default.Package(info, ioutil.Discard))
	assert.Equal(t, expectedArch, info.Arch)
}

func TestCreateBuilderData(t *testing.T) {
	info := exampleInfo()
	size := int64(0)
	builderData := createBuilderData(info, &size)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	assert.NoError(t, builderData(tw))

	assert.Equal(t, 8712, buf.Len())
}

func TestCombineToApk(t *testing.T) {
	var bufData bytes.Buffer
	bufData.Write([]byte{1})

	var bufControl bytes.Buffer
	bufControl.Write([]byte{2})

	var bufTarget bytes.Buffer

	assert.NoError(t, combineToApk(&bufTarget, &bufData, &bufControl))
	assert.Equal(t, 2, bufTarget.Len())
}

func TestPathsToCreate(t *testing.T) {
	for pathToTest, parts := range map[string][]string{
		"/usr/share/doc/whatever/foo.md": {"usr", "usr/share", "usr/share/doc", "usr/share/doc/whatever"},
		"/var/moises":                    {"var"},
		"/":                              []string(nil),
	} {
		parts := parts
		pathToTest := pathToTest
		t.Run(fmt.Sprintf("pathToTest: '%s'", pathToTest), func(t *testing.T) {
			assert.Equal(t, parts, pathsToCreate(pathToTest))
		})
	}
}

func TestDefaultWithArch(t *testing.T) {
	for _, arch := range []string{"386", "amd64"} {
		arch := arch
		t.Run(arch, func(t *testing.T) {
			info := exampleInfo()
			info.Arch = arch
			var err = Default.Package(info, ioutil.Discard)
			assert.NoError(t, err)
		})
	}
}

func TestNoInfo(t *testing.T) {
	var err = Default.Package(nfpm.WithDefaults(&nfpm.Info{}), ioutil.Discard)
	assert.NoError(t, err)
}

func TestFileDoesNotExist(t *testing.T) {
	var err = Default.Package(
		nfpm.WithDefaults(&nfpm.Info{
			Name:        "foo",
			Arch:        "amd64",
			Description: "Foo does things",
			Priority:    "extra",
			Maintainer:  "Carlos A Becker <pkg@carlosbecker.com>",
			Version:     "1.0.0",
			Section:     "default",
			Homepage:    "http://carlosbecker.com",
			Vendor:      "nope",
			Overridables: nfpm.Overridables{
				Depends: []string{
					"bash",
				},
				Files: map[string]string{
					"../testdata/": "/usr/local/bin/fake",
				},
				ConfigFiles: map[string]string{
					"../testdata/whatever.confzzz": "/etc/fake/fake.conf",
				},
			},
		}),
		ioutil.Discard,
	)
	assert.EqualError(t, err, "glob failed: ../testdata/whatever.confzzz: file does not exist")
}

func TestNoFiles(t *testing.T) {
	var err = Default.Package(
		nfpm.WithDefaults(&nfpm.Info{
			Name:        "foo",
			Arch:        "amd64",
			Description: "Foo does things",
			Priority:    "extra",
			Maintainer:  "Carlos A Becker <pkg@carlosbecker.com>",
			Version:     "1.0.0",
			Section:     "default",
			Homepage:    "http://carlosbecker.com",
			Vendor:      "nope",
			Overridables: nfpm.Overridables{
				Depends: []string{
					"bash",
				},
			},
		}),
		ioutil.Discard,
	)
	assert.NoError(t, err)
}

func TestCreateBuilderControl(t *testing.T) {
	info := exampleInfo()
	size := int64(12345)
	builderControl := createBuilderControl(info, size, sha256.New().Sum(nil))

	var w bytes.Buffer
	tw := tar.NewWriter(&w)
	assert.NoError(t, builderControl(tw))

	var control = string(extractFromTar(t, w.Bytes(), ".PKGINFO"))
	var golden = "testdata/TestCreateBuilderControl.golden"
	if *update {
		require.NoError(t, ioutil.WriteFile(golden, []byte(control), 0655)) // nolint: gosec
	}
	bts, err := ioutil.ReadFile(golden) //nolint:gosec
	assert.NoError(t, err)
	assert.Equal(t, string(bts), control)
}

func TestCreateBuilderControlScripts(t *testing.T) {
	info := exampleInfo()
	info.Scripts = nfpm.Scripts{
		PreInstall:  "../testdata/scripts/preinstall.sh",
		PostInstall: "../testdata/scripts/postinstall.sh",
		PreRemove:   "../testdata/scripts/preremove.sh",
		PostRemove:  "../testdata/scripts/postremove.sh",
	}

	size := int64(12345)
	builderControl := createBuilderControl(info, size, sha256.New().Sum(nil))

	var w bytes.Buffer
	tw := tar.NewWriter(&w)
	assert.NoError(t, builderControl(tw))

	var control = string(extractFromTar(t, w.Bytes(), ".PKGINFO"))
	var golden = "testdata/TestCreateBuilderControlScripts.golden"
	if *update {
		require.NoError(t, ioutil.WriteFile(golden, []byte(control), 0655)) // nolint: gosec
	}
	bts, err := ioutil.ReadFile(golden) //nolint:gosec
	assert.NoError(t, err)
	assert.Equal(t, string(bts), control)
}

func TestControl(t *testing.T) {
	var w bytes.Buffer
	assert.NoError(t, writeControl(&w, controlData{
		Info:          exampleInfo(),
		InstalledSize: 10,
	}))
	var golden = "testdata/TestControl.golden"
	if *update {
		require.NoError(t, ioutil.WriteFile(golden, w.Bytes(), 0655)) // nolint: gosec
	}
	bts, err := ioutil.ReadFile(golden) //nolint:gosec
	assert.NoError(t, err)
	assert.Equal(t, string(bts), w.String())
}

func TestSignature(t *testing.T) {
	info := exampleInfo()
	info.APK.Signature.KeyFile = "../internal/sign/testdata/rsa.priv"
	info.APK.Signature.KeyName = "testkey.rsa.pub"
	info.APK.Signature.KeyPassphrase = "hunter2"

	digest := sha1.New().Sum(nil) // nolint:gosec

	var signatureTarGz bytes.Buffer
	tw := tar.NewWriter(&signatureTarGz)
	require.NoError(t, createSignatureBuilder(digest, info)(tw))

	signature := extractFromTar(t, signatureTarGz.Bytes(), ".SIGN.RSA.testkey.rsa.pub")
	err := sign.RSAVerifySHA1Digest(digest, signature, "../internal/sign/testdata/rsa.pub")
	require.NoError(t, err)

	err = Default.Package(info, ioutil.Discard)
	require.NoError(t, err)
}

func TestSignatureError(t *testing.T) {
	info := exampleInfo()
	info.APK.Signature.KeyFile = "../internal/sign/testdata/rsa.priv"
	info.APK.Signature.KeyName = "testkey.rsa.pub"
	info.APK.Signature.KeyPassphrase = "hunter2"

	// wrong hash format
	digest := sha256.New().Sum(nil)

	var signatureTarGz bytes.Buffer

	err := createSignature(&signatureTarGz, info, digest)
	require.Error(t, err)

	var expectedError *nfpm.ErrSigningFailure
	require.True(t, errors.As(err, &expectedError))

	_, ok := err.(*nfpm.ErrSigningFailure)
	assert.True(t, ok)
}

func extractFromTar(t *testing.T, tarFile []byte, fileName string) []byte {
	t.Helper()

	var tr = tar.NewReader(bytes.NewReader(tarFile))

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		if hdr.Name != fileName {
			continue
		}

		data, err := ioutil.ReadAll(tr)
		require.NoError(t, err)
		return data
	}

	t.Fatalf("file %q not found in tar file", fileName)
	return nil
}
