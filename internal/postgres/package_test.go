// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package postgres

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/discovery/internal"
	"golang.org/x/discovery/internal/derrors"
	"golang.org/x/discovery/internal/sample"
	"golang.org/x/discovery/internal/source"
	"golang.org/x/discovery/internal/stdlib"
	"golang.org/x/xerrors"
)

func TestGetPackage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	defer ResetTestDB(testDB, t)

	mustInsertVersion := func(pkgPath, modulePath, version string) {
		t.Helper()
		v := sample.Version()
		v.ModulePath = modulePath
		v.Version = version
		pkg := sample.Package()
		pkg.Path = pkgPath
		v.Packages = []*internal.Package{pkg}
		if err := testDB.InsertVersion(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	checkPackage := func(got *internal.VersionedPackage, pkgPath, modulePath, version string) {
		t.Helper()
		want := sample.VersionedPackage()
		want.Imports = nil
		want.ModulePath = modulePath
		want.Path = pkgPath
		want.Version = version
		if diff := cmp.Diff(want, got, cmpopts.EquateEmpty(), cmp.AllowUnexported(source.Info{})); diff != "" {
			t.Errorf("testDB.GetPackage(ctx, %q, %q, %q) mismatch (-want +got):\n%s", pkgPath, modulePath, version, diff)
		}
	}

	for _, data := range []struct {
		pkgPath, modulePath, version string
	}{
		{
			"github.com/hashicorp/vault/api",
			"github.com/hashicorp/vault",
			"v1.1.2",
		},
		{
			"github.com/hashicorp/vault/api",
			"github.com/hashicorp/vault/api",
			"v1.0.3",
		},
		{
			"github.com/hashicorp/vault/api",
			"github.com/hashicorp/vault",
			"v1.0.3",
		},
		{
			"github.com/hashicorp/vault/api",
			"github.com/hashicorp/vault",
			"v1.1.0-alpha.1",
		},
		{
			"github.com/hashicorp/vault/api",
			"github.com/hashicorp/vault",
			"v1.0.0-20190311183353-d8887717615a",
		},
		{
			"archive/zip",
			stdlib.ModulePath,
			"v1.13.1",
		},
		{
			"archive/zip",
			stdlib.ModulePath,
			"v1.13.0",
		},
	} {
		mustInsertVersion(data.pkgPath, data.modulePath, data.version)
	}

	for _, tc := range []struct {
		name, pkgPath, modulePath, version, wantPkgPath, wantModulePath, wantVersion string
		wantNotFoundErr                                                              bool
	}{
		{
			name:           "want latest package to be most recent release version",
			pkgPath:        "github.com/hashicorp/vault/api",
			modulePath:     internal.UnknownModulePath,
			version:        internal.LatestVersion,
			wantPkgPath:    "github.com/hashicorp/vault/api",
			wantModulePath: "github.com/hashicorp/vault",
			wantVersion:    "v1.1.2",
		},
		{
			name:           "want package@version for ambigious module path to be longest module path",
			pkgPath:        "github.com/hashicorp/vault/api",
			modulePath:     internal.UnknownModulePath,
			version:        "v1.0.3",
			wantPkgPath:    "github.com/hashicorp/vault/api",
			wantModulePath: "github.com/hashicorp/vault/api",
			wantVersion:    "v1.0.3",
		},
		{
			name:           "want package with prerelease version and module path",
			pkgPath:        "github.com/hashicorp/vault/api",
			modulePath:     "github.com/hashicorp/vault",
			version:        "v1.1.0-alpha.1",
			wantPkgPath:    "github.com/hashicorp/vault/api",
			wantModulePath: "github.com/hashicorp/vault",
			wantVersion:    "v1.1.0-alpha.1",
		},
		{
			name:           "want package for pseudoversion, only one version for module path",
			pkgPath:        "github.com/hashicorp/vault/api",
			modulePath:     internal.UnknownModulePath,
			version:        "v1.1.0-alpha.1",
			wantPkgPath:    "github.com/hashicorp/vault/api",
			wantModulePath: "github.com/hashicorp/vault",
			wantVersion:    "v1.1.0-alpha.1",
		},
		{
			name:            "module@version/suffix does not exist ",
			pkgPath:         "github.com/hashicorp/vault/api",
			modulePath:      "github.com/hashicorp/vault/api",
			wantPkgPath:     "github.com/hashicorp/vault/api",
			version:         "v1.1.2",
			wantNotFoundErr: true,
		},
		{
			name:           "latest version of archive/zip",
			pkgPath:        "archive/zip",
			modulePath:     stdlib.ModulePath,
			version:        internal.LatestVersion,
			wantPkgPath:    "archive/zip",
			wantModulePath: stdlib.ModulePath,
			wantVersion:    "v1.13.1",
		},
		{
			name:           "specific version of archive/zip",
			pkgPath:        "archive/zip",
			modulePath:     stdlib.ModulePath,
			version:        "v1.13.0",
			wantPkgPath:    "archive/zip",
			wantModulePath: stdlib.ModulePath,
			wantVersion:    "v1.13.0",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := testDB.GetPackage(ctx, tc.pkgPath, tc.modulePath, tc.version)
			if tc.wantNotFoundErr {
				if !xerrors.Is(err, derrors.NotFound) {
					t.Fatalf("want derrors.NotFound; got = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			checkPackage(got, tc.wantPkgPath, tc.wantModulePath, tc.wantVersion)
		})
	}
}

func TestGetPackageInvalidArguments(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	defer ResetTestDB(testDB, t)

	for _, tc := range []struct {
		name, modulePath, version string
		wantInvalidArgumentErr    bool
	}{
		{
			name:                   "version cannot be empty",
			modulePath:             internal.UnknownModulePath,
			version:                "",
			wantInvalidArgumentErr: true,
		},
		{
			name:                   "module path cannot be empty",
			modulePath:             "",
			version:                internal.LatestVersion,
			wantInvalidArgumentErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := testDB.GetPackage(ctx, tc.modulePath+"/package", tc.modulePath, tc.version)
			if !xerrors.Is(err, derrors.InvalidArgument) {
				t.Fatalf("want %v; got = \n%+v, %v", derrors.InvalidArgument, got, err)
			}
		})
	}
}
