{ pkgs, ... }:

{
  languages.go.enable = true;
  packages = [ pkgs.gotools ];
}
