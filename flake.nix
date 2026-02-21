{
  description = "Kiro Gateway - Proxy for Amazon Q/CodeWhisperer API";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-24.05";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages.kiro-go-gw = pkgs.buildGoModule {
          pname = "kiro-go-gw";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";
          subPackages = [ "./cmd/server" ];
          CGO_ENABLED = 1;
          buildInputs = [ pkgs.sqlite ];
          nativeBuildInputs = [ pkgs.pkg-config ];
        };

        packages.default = self.packages.${system}.kiro-go-gw;
      }
    );
}
