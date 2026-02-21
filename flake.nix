{
  description = "Kiro Gateway - Proxy for Amazon Q/CodeWhisperer API";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    src.url = "github:kesor/kiro-go-gw";
    src.flake = false;
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      src,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages.kiro-go-gw = pkgs.buildGoApplication {
          pname = "kiro-go-gw";
          version = "0.1.0";
          src = src;
          go = pkgs.go_1_23;
          subPackages = [ "cmd/server" ];
          CGO_ENABLED = 1;
          buildInputs = [ pkgs.sqlite ];
          nativeBuildInputs = [ pkgs.pkg-config ];
        };

        packages.default = self.packages.${system}.kiro-go-gw;

        devShells.default = pkgs.mkShell {
          buildInputs = [
            pkgs.go_1_23
            pkgs.sqlite
            pkgs.pkg-config
          ];
        };
      }
    );
}
