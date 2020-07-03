package handlers

import (
	"context"

	lsctx "github.com/hashicorp/terraform-ls/internal/context"
	ilsp "github.com/hashicorp/terraform-ls/internal/lsp"
	"github.com/sourcegraph/go-lsp"
)

type ListRootModulePathsParams struct {
	TextDocument lsp.TextDocumentIdentifier
}

type ListRootModulePathsResponse struct {
	Paths []string
}

func ListRootModules(ctx context.Context, params *ListRootModulePathsParams) (*ListRootModulePathsResponse, error) {
	cf, err := lsctx.RootModuleCandidateFinder(ctx)
	if err != nil {
		return nil, err
	}

	fh := ilsp.FileHandlerFromDocumentURI(params.TextDocument.URI)

	candidates := cf.RootModuleCandidatesByPath(fh.Dir())

	return &ListRootModulePathsResponse{
		Paths: candidates,
	}, nil
}
