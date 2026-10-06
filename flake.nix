{
  description = "kitafino-cli: kitafino lunch ordering from the terminal and as MCP server";

  inputs = {
    nixpkgs.url = "nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      ...
    }:
    let
      version = self.shortRev or self.dirtyShortRev or "dev";
      # Kept next to go.mod so dependency updates never edit this file;
      # `nix run .#vendor-hash` rewrites it.
      vendorHash = nixpkgs.lib.fileContents ./go.mod.sri;
    in
    {
      nixosModules.default = import ./module.nix self;
    }
    // flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        buildGoModule = pkgs.buildGoModule.override { go = pkgs.go_latest; };

        kitafino-cli = buildGoModule {
          pname = "kitafino-cli";
          inherit version vendorHash;
          src = ./.;
          env.CGO_ENABLED = 0;
          ldflags = [
            "-s"
            "-w"
          ];
          meta.mainProgram = "kitafino-cli";
        };
      in
      {
        packages = {
          inherit kitafino-cli;
          default = kitafino-cli;
        };

        apps.default = flake-utils.lib.mkApp { drv = kitafino-cli; };

        # Same NAR hash buildGoModule computes, without a failing build first.
        apps.vendor-hash = flake-utils.lib.mkApp {
          drv = pkgs.writeShellApplication {
            name = "vendor-hash";
            runtimeInputs = [ pkgs.go_latest ];
            text = ''
              out=$(mktemp -d)
              trap 'rm -rf "$out"' EXIT
              rm -rf "$out"
              go mod vendor -o "$out"
              go run tailscale.com/cmd/nardump@v1.102.5 --sri "$out" > go.mod.sri
              cat go.mod.sri
            '';
          };
        };

        devShells.default = pkgs.mkShell {
          packages = [
            pkgs.go_latest
            pkgs.gopls
            pkgs.gotools
          ];
        };

        formatter = pkgs.nixfmt;

        checks = {
          # The package build runs go test.
          build = kitafino-cli;
          gofmt = pkgs.runCommand "gofmt" { nativeBuildInputs = [ pkgs.go_latest ]; } ''
            unformatted=$(gofmt -l ${./.})
            [ -z "$unformatted" ] || { echo "not gofmt'ed: $unformatted"; exit 1; }
            touch $out
          '';
        };
      }
    );
}
