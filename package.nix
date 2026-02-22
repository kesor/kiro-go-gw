{
  buildGoModule,
  lib,
  src,
  vendorHash,
  sqlite,
  pkg-config,
}:

let
  version = "0.1.0";
in
buildGoModule {
  inherit src vendorHash;
  pname = "kiro-go-gw";
  inherit version;

  CGO_ENABLED = lib.mkDefault 1;

  ldflags = [
    "-X main.version=${version}"
  ];

  subPackages = [ "cmd/server" ];
  buildInputs = [ sqlite ];
  nativeBuildInputs = [ pkg-config ];
}
