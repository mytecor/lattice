{ config, lib, pkgs, modulesPath, nixos-hardware, ... }:

{
  imports = [
    nixos-hardware.nixosModules.common-pc
    nixos-hardware.nixosModules.common-cpu-intel
    (modulesPath + "/installer/scan/not-detected.nix")
  ];

  nixpkgs.hostPlatform = "x86_64-linux";

  boot.initrd.availableKernelModules = [ "xhci_pci" "ahci" "usbhid" "usb_storage" "sd_mod" ];
  boot.kernelModules = [ "kvm-intel" ];
  hardware.cpu.intel.updateMicrocode = lib.mkDefault config.hardware.enableRedistributableFirmware;

  # Suppress Realtek RTL8822CE PCIe AER error floods and disable link ASPM
  boot.kernelParams = [ "pci=noaer" ];
  boot.extraModprobeConfig = ''
    options rtw88_pci disable_aspm=y
  '';

  services.udev.extraRules = ''
    ACTION=="add", SUBSYSTEM=="pci", ATTR{vendor}=="0x10ec", ATTR{device}=="0xc822", ATTR{link/l1_aspm}="0", ATTR{link/l1_1_aspm}="0", ATTR{link/l1_2_aspm}="0", ATTR{link/l0s_aspm}="0", ATTR{link/clkpm}="0"
  '';

  # Intel UHD Graphics (Alder Lake-N)
  hardware.graphics = {
    enable = true;
    extraPackages = with pkgs; [
      intel-media-driver    # VAAPI (iHD)
      intel-compute-runtime # OpenCL
      vpl-gpu-rt            # Intel Video Processing Library
    ];
  };
}
