{ pkgs, module, ... }:
pkgs.testers.runNixOSTest {
  name = "comin-niks3";
  sshBackdoor.enable = true;
  nodes.machine = { pkgs, ... }: {
    imports = [ module ];
    virtualisation.graphics = false;
    # Serves /srv/pins/<name> as plain text, like the niks3 read proxy.
    systemd.services.pin-server = {
      wantedBy = [ "multi-user.target" ];
      preStart = "mkdir -p /srv/pins";
      serviceConfig.ExecStart = "${pkgs.python3}/bin/python3 -m http.server 8080 --bind 127.0.0.1 --directory /srv";
    };
    services.comin = {
      enable = true;
      fetcher = {
        type = "niks3";
        niks3.remotes = {
          main = {
            url = "http://127.0.0.1:8080/pins/main";
            operation = "test";
            poller.period = 2;
          };
        };
      };
    };
    system.stateVersion = "26.05";
  };
  testScript = ''
    start_all()

    machine.wait_for_unit("pin-server.service")
    machine.succeed("systemctl start comin")
    machine.wait_for_unit("comin.service")
    machine.succeed("systemctl is-active comin")
    machine.wait_until_succeeds("comin status", 10)

    # No pin yet: comin keeps polling and deploys nothing.
    machine.wait_until_succeeds("journalctl -u comin | grep -q 'failed to fetch remote main'", 30)

    # Pin the running system, as CI does with `niks3 push --pin`.
    machine.succeed("readlink -f /run/current-system > /srv/pins/main")
    machine.wait_until_succeeds("comin status | grep -q 'Deployment succeeded'", 120)
    machine.succeed("comin status | grep -q \"Outpath $(readlink -f /run/current-system)\"")

    # The exporter reports the deployed toplevel.
    machine.wait_until_succeeds(
      "${pkgs.curl}/bin/curl -fsS http://127.0.0.1:4243/metrics"
      + " | grep '^comin_deployment_info{'"
      + " | grep -F \"out_path=\\\"$(readlink -f /run/current-system)\\\"\""
      + " | grep -qF 'status=\"done\"'",
      30,
    )
  '';
}
