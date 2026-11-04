/* content.js — guides, tutorials and glossary for the Developer Workspaces portal.
   Plain language on purpose: short sentences, one idea per step, every new word
   has a popup (data-topic="…") that explains it and links to the official docs.
   Edit freely; nothing here is required by the API. */
"use strict";

window.CONTENT = (() => {
  // t('ssh') renders an inline term with an (i) popup button.
  // t("ssh", "SSH") → underlined term with an (i); t("ssh", "") → the (i) alone (icon-only, labelled for screen readers).
  const t = (id, label) => {
    const text = label === undefined ? id : label;
    return text
      ? `<button type="button" class="term" data-topic="${id}">${text}<span class="term-i" aria-hidden="true">i</span></button>`
      : `<button type="button" class="term term-only" data-topic="${id}" aria-label="What is this? (${id})" title="What is this?"><span class="term-i">i</span></button>`;
  };
  const code = (s) => `<code>${s}</code>`;
  const cmd = (s) => `<pre class="cmd"><code>${s}</code></pre>`;
  const note = (s) => `<div class="callout callout-info">${s}</div>`;
  const warn = (s) => `<div class="callout callout-warn">${s}</div>`;
  const tip = (s) => `<div class="callout callout-ok">${s}</div>`;

  /* ------------------------------------------------------------------ glossary */
  const topics = {
    workspace: {
      title: "What is a workspace?",
      short: "Your own Linux computer running in the company's cloud.",
      body: `<p>A workspace is a complete Linux computer that runs in our Kubernetes cluster instead of on your desk. It has a CPU, memory, a disk, the programming languages you picked, and your code.</p>
<p>Think of it like a locker room at school: you get your own locker (the <strong>home volume</strong>) that keeps your things, and a desk (the <strong>pod</strong>) that is set up fresh every morning. If the desk is thrown away and a new one is set up, your locker is still there with everything in it.</p>
<ul><li>You connect to it with ${t("ssh", "SSH")} — from a terminal or from VS Code.</li><li>It turns itself off when you stop using it, and turns back on when you connect.</li><li>You are <em>root</em> inside it (you can install anything), but it cannot touch other people's workspaces.</li></ul>`,
      links: [["Kubernetes: what is a Pod?", "https://kubernetes.io/docs/concepts/workloads/pods/"], ["VS Code Remote Development", "https://code.visualstudio.com/docs/remote/remote-overview"]],
    },
    container: {
      title: "Container",
      short: "A packaged program with everything it needs to run.",
      body: `<p>A container is a box that holds a program <em>and</em> all the files it needs: the operating system files, libraries, and tools. Because the box is complete, it runs the same way on any computer.</p><p>Your workspace is one container. It was built from an ${t("image")} that our platform team prepared.</p>`,
      links: [["Docker: what is a container?", "https://www.docker.com/resources/what-container/"], ["OCI image specification", "https://github.com/opencontainers/image-spec"]],
    },
    image: {
      title: "Image (container image)",
      short: "The recipe and ingredients a workspace starts from.",
      body: `<p>An image is a frozen snapshot of a file system: Ubuntu Linux plus the compilers, languages and tools. Every time your workspace starts, a fresh copy of that image is unpacked. Your own files are not in the image — they live on your ${t("volume")}.</p><p>We keep one image per main language (Python, Go, Rust, Java, Node.js, C/C++). Picking more languages adds them on top when the workspace boots. All images are stored in ${t("artifactory", "Artifactory")}.</p>`,
      links: [["Dockerfile reference", "https://docs.docker.com/reference/dockerfile/"]],
    },
    pod: {
      title: "Pod",
      short: "The running copy of your workspace in Kubernetes.",
      body: `<p>Kubernetes calls a running container (or a small group of them) a <strong>pod</strong>. When your workspace is <em>Running</em>, a pod exists. When it is <em>Stopped</em>, the pod is gone but your ${t("volume")} remains. Starting the workspace simply creates a new pod and attaches the same volume.</p>`,
      links: [["Kubernetes Pods", "https://kubernetes.io/docs/concepts/workloads/pods/"]],
    },
    kubernetes: {
      title: "Kubernetes",
      short: "The system that runs thousands of containers on many servers.",
      body: `<p>Kubernetes (often written <em>K8s</em>) is the software that decides which server runs your workspace, gives it CPU and memory, attaches your disk, and restarts things when they fail. You do not need to learn it to use workspaces — the portal and the ${code("workspace")} command do the talking for you.</p>`,
      links: [["Kubernetes overview", "https://kubernetes.io/docs/concepts/overview/"]],
    },
    namespace: {
      title: "Namespace (your area of the cluster)",
      short: "A private area in Kubernetes that only you can use.",
      body: `<p>Every developer gets a namespace named ${code("ws-&lt;your name&gt;")}. Everything you create lives there, and only you (and platform admins) can see it. Developers who do hardware work also get ${code("ws-&lt;your name&gt;-lab")}, where more privileges are allowed.</p><p>Your namespace has a <strong>quota</strong>: a maximum amount of CPU, memory and disk for all your workspaces together.</p>`,
      links: [["Kubernetes Namespaces", "https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/"], ["Resource quotas", "https://kubernetes.io/docs/concepts/policy/resource-quotas/"]],
    },
    volume: {
      title: "Home volume (persistent disk)",
      short: "Your files, kept safe even when the workspace is off.",
      body: `<p>Your home folder ${code("/home/dev")} is on a network disk called a <strong>PersistentVolumeClaim</strong> (PVC). Code in ${code("~/workspace")}, your shell history, downloaded packages, VS Code's extensions — all of it is on this disk.</p>
<p>When the workspace stops, the disk is detached, not deleted. When it starts again, the disk is attached to the new pod. Only <em>Delete → also delete my files</em> removes it.</p>
${warn("Anything <strong>outside</strong> your home folder (for example <code>sudo apt install</code>) is rebuilt from the image at every start. Add such tools from the Tools list instead, so they come back automatically.")}`,
      links: [["Persistent Volumes", "https://kubernetes.io/docs/concepts/storage/persistent-volumes/"]],
    },
    ssh: {
      title: "SSH (Secure Shell)",
      short: "The safe way to open a terminal on a computer far away.",
      body: `<p>SSH lets your laptop type commands on another computer over an encrypted connection — nobody in between can read it. VS Code's <em>Remote-SSH</em> feature uses SSH so the editor runs on your laptop while the files and programs run in the workspace.</p><p>SSH proves who you are with an ${t("sshkey", "SSH key")} instead of a password.</p>`,
      links: [["OpenSSH manual", "https://man.openbsd.org/ssh"], ["VS Code Remote-SSH", "https://code.visualstudio.com/docs/remote/ssh"]],
    },
    sshkey: {
      title: "SSH key (public and private)",
      short: "A pair of files that act like a lock and a key.",
      body: `<p>An SSH key comes in two parts:</p><ul><li>The <strong>private key</strong> stays on your laptop. Never share it, never paste it anywhere.</li><li>The <strong>public key</strong> is the matching lock. You give it to the workspace (through this portal) so your private key can open it.</li></ul>
<p>The public key is one line of text that starts with ${code("ssh-ed25519")}. Make a pair with:</p>${cmd("ssh-keygen -t ed25519 -C \"your-email@example.com\"")}<p>Then show the public part with ${code("cat ~/.ssh/id_ed25519.pub")} and paste it in your <a href="#/profile">Profile</a>.</p>`,
      links: [["GitHub: generating a new SSH key", "https://docs.github.com/en/authentication/connecting-to-github-with-ssh/generating-a-new-ssh-key-and-adding-it-to-the-ssh-agent"], ["ssh-keygen manual", "https://man.openbsd.org/ssh-keygen"]],
    },
    hostkey: {
      title: "Host key and known_hosts",
      short: "How your laptop knows it is talking to the real workspace.",
      body: `<p>Just like you prove who you are with a key, the workspace proves who <em>it</em> is with a <strong>host key</strong>. The first time you connect, SSH saves the host key in ${code("~/.ssh/known_hosts")}. If it ever changes unexpectedly, SSH warns you — that is a good thing.</p><p>Each workspace keeps the same host key for its whole life, even after stop/start, so you only ever see the question once. The portal shows the fingerprint on the workspace page so you can compare.</p>`,
      links: [["SSH host keys explained", "https://www.ssh.com/academy/ssh/host-key"]],
    },
    proxycommand: {
      title: "How the connection reaches the workspace",
      short: "SSH travels inside your normal Kubernetes login.",
      body: `<p>Workspaces are not on the public internet. Your SSH connection is wrapped inside a Kubernetes connection (the same one ${code("kubectl")} uses). The ${code("workspace")} command sets this up in your ${code("~/.ssh/config")} as a <em>ProxyCommand</em>. VS Code and plain ${code("ssh")} both understand it.</p><p>Bonus: if the workspace is stopped, connecting starts it automatically.</p>`,
      links: [["ssh_config ProxyCommand", "https://man.openbsd.org/ssh_config#ProxyCommand"], ["kubectl exec", "https://kubernetes.io/docs/reference/kubectl/generated/kubectl_exec/"]],
    },
    portforward: {
      title: "Port forwarding",
      short: "See a web app running in the workspace from your laptop's browser.",
      body: `<p>If your program listens on a port (for example a web server on port 8080) inside the workspace, port forwarding makes ${code("http://localhost:8080")} on your laptop go to it. In VS Code this is the <strong>Ports</strong> panel — it usually detects new ports by itself. From a terminal:</p>${cmd("ssh -L 8080:localhost:8080 ws-myproject")}`,
      links: [["VS Code: forwarding a port", "https://code.visualstudio.com/docs/editor/port-forwarding"]],
    },
    cli: {
      title: "The workspace command (CLI)",
      short: "The terminal version of this website.",
      body: `<p>${code("workspace")} is a small command-line program that does the same things as this portal: ${code("workspace create --lang rust --tools cmake,git")}, ${code("workspace ssh rust")}, ${code("workspace stop rust")}. You need it for one thing even if you prefer the portal: ${code("workspace ssh-config &lt;name&gt;")} writes the SSH settings that let VS Code connect.</p><p>Install: copy the single file to your PATH (ask in the help channel for the download link), and make sure ${code("kubectl")} works for you.</p>`,
      links: [["Install kubectl", "https://kubernetes.io/docs/tasks/tools/"]],
    },
    git: {
      title: "Git",
      short: "The tool that tracks every change to your code.",
      body: `<p>Git keeps a history of your code so you can go back, work in branches, and share with teammates through a <strong>repository</strong> (repo) on GitHub, GitLab or Bitbucket. The usual loop is: ${code("git pull")} (get the latest), edit, ${code("git commit")} (save a step), ${code("git push")} (send it up).</p><p>Your workspace can clone a repo for you on the first start.</p>`,
      links: [["Pro Git book (free)", "https://git-scm.com/book/en/v2"], ["GitHub Skills: introduction to GitHub", "https://skills.github.com/"]],
    },
    deploykey: {
      title: "Deploy key",
      short: "A key that belongs to one workspace, for pulling and pushing code.",
      body: `<p>When you create a workspace, it gets its own ${t("sshkey", "SSH key")} for Git. The private half lives only in the cluster; the portal shows you the public half. Add that public key to your repository (<em>Settings → Deploy keys</em>, tick <em>Allow write access</em>) or to your Git account's SSH keys. From then on the workspace can clone, pull and push without any password.</p><p>Why not copy your personal key into the cluster? Because then the cluster would hold a key that opens <em>everything</em> of yours. A deploy key opens only what you allow.</p>`,
      links: [["GitHub: managing deploy keys", "https://docs.github.com/en/authentication/connecting-to-github-with-ssh/managing-deploy-keys"], ["GitLab: deploy keys", "https://docs.gitlab.com/ee/user/project/deploy_keys/"]],
    },
    token: {
      title: "Access token (HTTPS)",
      short: "A long password made for programs, usable instead of a deploy key.",
      body: `<p>If your repository uses ${code("https://")} URLs, you can give the workspace an access token instead of a key. Make a token with only the permissions you need (read and write to the repo) and an expiry date. The workspace stores it in a Kubernetes Secret and hands it to Git only when Git asks.</p>`,
      links: [["GitHub: fine-grained personal access tokens", "https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens"], ["GitLab: project access tokens", "https://docs.gitlab.com/ee/user/project/settings/project_access_tokens.html"]],
    },
    artifactory: {
      title: "Artifactory",
      short: "Our company's download store for images, packages and tools.",
      body: `<p>JFrog Artifactory is a server that stores software: container ${t("image", "images")}, Python and npm packages, Go modules, Java libraries, and plain files such as tool downloads. It also <em>caches</em> the public internet: the first person to download a package pulls it from outside; everyone after that gets the copy from Artifactory, fast and scanned for problems.</p>
<p>In workspaces, everything comes from Artifactory:</p><ul><li>The workspace image itself.</li><li>Tools you tick in the <em>Tools</em> step (they are files in Artifactory).</li><li>${code("pip")}, ${code("npm")}, ${code("go get")}, ${code("cargo")}, ${code("mvn")} and ${code("apt")} are pre-configured to use it.</li></ul>
<p>A <strong>repository</strong> in Artifactory is a shelf: ${code("pypi-remote")} mirrors PyPI, ${code("github-remote")} mirrors GitHub releases, ${code("devspaces-docker")} holds our images.</p>`,
      links: [["JFrog Artifactory documentation", "https://jfrog.com/help/r/jfrog-artifactory-documentation"], ["Remote repositories", "https://jfrog.com/help/r/jfrog-artifactory-documentation/remote-repositories"]],
    },
    packagemanager: {
      title: "Package manager",
      short: "A tool that installs libraries for you (pip, npm, cargo, apt…).",
      body: `<p>Instead of downloading libraries by hand, every language has a package manager: ${code("pip")} for Python, ${code("npm")} for Node.js, ${code("cargo")} for Rust, ${code("go get")} for Go, ${code("mvn")}/${code("gradle")} for Java and ${code("apt")} for Ubuntu itself. In a workspace they are all pointed at ${t("artifactory", "Artifactory")}, so they work even without direct internet access.</p>`,
      links: [["pip user guide", "https://pip.pypa.io/en/stable/user_guide/"], ["npm docs", "https://docs.npmjs.com/"], ["Cargo book", "https://doc.rust-lang.org/cargo/"]],
    },
    idle: {
      title: "Idle timeout (auto-stop)",
      short: "Workspaces switch off by themselves when nobody is using them.",
      body: `<p>A running workspace costs CPU and memory that others could use. So every minute the workspace checks: is someone connected over SSH, or is a VS Code server running? If the answer has been <em>no</em> for longer than the idle timeout (4 hours by default), the workspace stops. Your files are safe; the next connection starts it again (about a minute).</p><p>Running a long job overnight? Set the timeout to <em>off</em> for that workspace, or keep a ${code("tmux")} session open.</p>`,
      links: [["tmux getting started", "https://github.com/tmux/tmux/wiki/Getting-Started"]],
    },
    root: {
      title: "root, sudo and user namespaces",
      short: "You are the administrator inside your workspace — safely.",
      body: `<p>Inside the workspace you log in as ${code("dev")} and can run ${code("sudo")} without a password, so ${code("sudo apt install …")} just works. Linux <strong>user namespaces</strong> make this safe: your <em>root</em> is real inside the box but is an ordinary, unprivileged user from the server's point of view. Even a mistake as root cannot reach other workspaces or the server.</p>`,
      links: [["Kubernetes: user namespaces", "https://kubernetes.io/docs/concepts/workloads/pods/user-namespaces/"], ["sudo manual", "https://www.sudo.ws/docs/man/sudo.man/"]],
    },
    sandbox: {
      title: "Sandboxes: user namespaces, gVisor and Kata",
      short: "Three ways to keep a workspace in its own box.",
      body: `<ul><li><strong>User namespace</strong> (default) — same Linux kernel as the server, but your root is remapped to a harmless user. Fast, and all normal programs work.</li><li><strong>gVisor</strong> — a tiny guard program sits between your workspace and the kernel and checks every system call. Very safe; a few unusual network features do not work.</li><li><strong>Kata</strong> — a very small virtual machine with its own kernel. Used for the <em>Kernel lab</em> profile, where you may want to crash a kernel on purpose.</li></ul>`,
      links: [["gVisor", "https://gvisor.dev/docs/"], ["Kata Containers", "https://katacontainers.io/docs/"]],
    },
    multicast: {
      title: "Multicast",
      short: "One sender, many receivers — like a radio station.",
      body: `<p>Most network traffic is <strong>unicast</strong>: one computer talks to one other computer, like a phone call. <strong>Multicast</strong> is like a radio station: a sender transmits once to a <em>group address</em> (for example ${code("239.255.0.42")}), and every computer that has <em>tuned in</em> to that group receives it. Market-data feeds, video streaming and some industrial protocols work this way.</p>
<p>Tuning in is called <em>joining a group</em>; the network protocol for it is ${code("IGMP")}. Normal Kubernetes networking does not carry multicast, so workspaces that need it get a second network card (${code("net1")}) on a special network that does.</p><p>Turn it on with the <em>Multicast network</em> switch in the Advanced step, add the <strong>mcast</strong> tool, and run ${code("mcast-check")} inside the workspace to test it.</p>`,
      links: [["RFC 1112: Host extensions for IP multicasting", "https://www.rfc-editor.org/rfc/rfc1112"], ["IGMP (Wikipedia)", "https://en.wikipedia.org/wiki/Internet_Group_Management_Protocol"], ["Multus CNI", "https://github.com/k8snetworkplumbingwg/multus-cni"]],
    },
    driver: {
      title: "Device driver",
      short: "The piece of software that lets the operating system talk to a piece of hardware.",
      body: `<p>A device driver teaches the Linux ${t("kernel")} how to use a device: a USB serial adapter, a camera, a network card, a GPU. Drivers run <em>inside</em> the kernel, so a bug in a driver can crash the whole machine. That is why driver development happens in the <strong>lab tier</strong>: special servers where your workspace may have full access to the hardware and nobody else is on the same machine.</p>`,
      links: [["Linux Device Drivers (free book)", "https://lwn.net/Kernel/LDD3/"], ["Kernel documentation: driver API", "https://www.kernel.org/doc/html/latest/driver-api/index.html"]],
    },
    kernel: {
      title: "Kernel",
      short: "The core of the operating system that talks to the hardware.",
      body: `<p>The kernel is the part of Linux that runs first and controls everything: CPU time, memory, disks, networks and devices. Programs ask the kernel for things through <em>system calls</em>. A workspace normally shares the server's kernel; the <strong>Kernel lab</strong> profile gives you your own kernel inside a small virtual machine so you can load and crash modules safely.</p>`,
      links: [["The Linux Kernel documentation", "https://www.kernel.org/doc/html/latest/"]],
    },
    privileged: {
      title: "Privileged workspace (hardware profile)",
      short: "Full power over the server's devices — with guardrails.",
      body: `<p>A <em>privileged</em> container can see the server's ${code("/dev")} devices, load kernel modules, and change kernel settings. It is the only way to develop some drivers, so the <strong>Hardware lab</strong> profile allows it — but only on the lab server pool, only one such workspace per server, and never with access to the cluster's own control files.</p>`,
      links: [["Pod Security Standards", "https://kubernetes.io/docs/concepts/security/pod-security-standards/"]],
    },
    deviceplugin: {
      title: "Device plugins, VFIO and SR-IOV",
      short: "How Kubernetes hands a real device to your workspace.",
      body: `<ul><li><strong>Device plugin</strong> — a helper that advertises devices on a server (GPUs, serial ports, cameras). You ask for ${code("nvidia.com/gpu: 1")} and Kubernetes places your workspace on a server that has one and passes it in.</li><li><strong>SR-IOV</strong> — a network card split into several small virtual cards (<em>VFs</em>), each given to a different workspace. Good for network-driver work.</li><li><strong>VFIO</strong> — hands a whole PCI card to a virtual machine (the Kernel lab profile), so your own kernel drives it directly.</li></ul>`,
      links: [["Kubernetes device plugins", "https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/"], ["NVIDIA device plugin", "https://github.com/NVIDIA/k8s-device-plugin"], ["VFIO documentation", "https://docs.kernel.org/driver-api/vfio.html"]],
    },
    hugepages: {
      title: "Hugepages",
      short: "Big memory pages for programs that move data very fast.",
      body: `<p>Memory is normally handed out in small 4 KB pages. Programs such as DPDK network drivers or databases prefer 2 MB or 1 GB <em>huge pages</em> because the CPU can address them faster. Tick <em>Hugepages</em> in the hardware step and they appear at ${code("/dev/hugepages")}.</p>`,
      links: [["Kubernetes: managing hugepages", "https://kubernetes.io/docs/tasks/manage-hugepages/scheduling-hugepages/"], ["DPDK getting started", "https://doc.dpdk.org/guides/linux_gsg/"]],
    },
    cpu: {
      title: "CPU, memory and disk sizes",
      short: "How much power your workspace gets.",
      body: `<p><strong>CPU</strong> = how many processor cores you can use at once (compiling big projects likes more). <strong>Memory</strong> (RAM) = how much your programs can hold while running (Java, browsers and large builds like more). <strong>Disk</strong> = how much your home volume can store (code, dependencies, build output).</p><p>Sizes you ask for count against your namespace quota. The <em>request</em> is what is reserved for you; the <em>limit</em> is the most you can burst to.</p>`,
      links: [["Kubernetes: resource requests and limits", "https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/"]],
    },
    vscode: {
      title: "VS Code Remote-SSH",
      short: "Edit files in the workspace as if they were on your laptop.",
      body: `<p>Visual Studio Code can run its editor window on your laptop and everything else (files, terminal, debugger, extensions) inside the workspace. Install the <strong>Remote - SSH</strong> extension, press <kbd>F1</kbd>, choose <em>Remote-SSH: Connect to Host…</em> and pick ${code("ws-&lt;name&gt;")}. The first connection installs a small VS Code server in the workspace (kept on your volume, so later connections are quick).</p>`,
      links: [["VS Code Remote-SSH tutorial", "https://code.visualstudio.com/docs/remote/ssh-tutorial"], ["Remote-SSH troubleshooting", "https://code.visualstudio.com/docs/remote/troubleshooting"]],
    },
    terminal: {
      title: "Terminal and shell",
      short: "Typing commands instead of clicking.",
      body: `<p>A terminal is a window where you type commands; the <em>shell</em> (in workspaces: ${code("bash")}) reads them and runs programs. Useful first commands: ${code("ls")} (list files), ${code("cd folder")} (go into a folder), ${code("pwd")} (where am I), ${code("cat file")} (show a file). Your command history is saved on your volume, so <kbd>↑</kbd> still finds yesterday's commands.</p>`,
      links: [["Ubuntu: the Linux command line for beginners", "https://ubuntu.com/tutorials/command-line-for-beginners"]],
    },
    dotfiles: {
      title: "Dotfiles",
      short: "Your personal settings (.bashrc, .gitconfig, editor config).",
      body: `<p>Files whose names start with a dot (${code(".bashrc")}, ${code(".vimrc")}, ${code(".gitconfig")}) hold personal settings. Many developers keep them in a Git repository. Give that repository's URL in the <em>Code</em> step and the workspace clones it on first boot and runs its ${code("install.sh")} if there is one.</p>`,
      links: [["GitHub: dotfiles guide", "https://dotfiles.github.io/"]],
    },
    yaml: {
      title: "What the portal creates behind the scenes",
      short: "Plain Kubernetes objects — nothing magic.",
      body: `<p>When you click <em>Create</em>, the portal writes five ordinary Kubernetes objects into your namespace: a <strong>PersistentVolumeClaim</strong> (your disk), a <strong>Secret</strong> with SSH keys, a <strong>Secret</strong> with Git credentials, a <strong>Service</strong>, and a <strong>StatefulSet</strong> that runs the pod. The ${code("workspace")} CLI creates exactly the same objects, so you can switch between the two at any time, and admins can inspect everything with ${code("kubectl")}.</p>`,
      links: [["StatefulSets", "https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/"]],
    },
    drive: {
      title: "Home drive",
      short: "The disk behind /home/dev. It outlives the workspace.",
      body: `<p>Every workspace has a <strong>home drive</strong>: a real cloud disk mounted at ${code("/home/dev")}. Your code, your shell history, your downloaded dependencies — all on the drive.</p>
<p>School analogy: the workspace is a desk in a classroom that gets wiped and rearranged every evening. The drive is your locker. Nobody empties your locker because the desk was cleaned.</p>
<ul><li><strong>Stop</strong> a workspace → the desk is gone, the locker stays.</li><li><strong>Delete</strong> a workspace → same: the drive becomes <em>detached</em> and shows on the Drives page. Put a new workspace on it any time, even with different languages.</li><li><strong>A pod crashes or a node dies</strong> → Kubernetes starts a new pod on the same drive.</li><li><strong>Delete drive</strong> (Drives page) or ${code("--purge")} on the CLI is the <em>only</em> thing that erases it. The platform even has an admission rule that refuses to delete a home drive by accident.</li></ul>
<p>You pick the kind of disk ("Fast SSD", "Standard"…) in the Size step; on Azure these are Premium and Standard SSD managed disks, created with <em>Retain</em> so the cloud keeps the disk until an admin removes it.</p>`,
      links: [["Kubernetes: Persistent Volumes", "https://kubernetes.io/docs/concepts/storage/persistent-volumes/"], ["Azure Disks CSI driver", "https://learn.microsoft.com/azure/aks/azure-disk-csi"]],
    },
    snapshot: {
      title: "Snapshots",
      short: "A frozen copy of a drive you can turn into a new drive later.",
      body: `<p>A snapshot is a point-in-time copy of a home drive, taken by the storage system (${code("VolumeSnapshot")} through the CSI driver — on Azure an incremental disk snapshot that only stores the changed blocks).</p>
<p>Use it before something risky (a big upgrade, a tool that rewrites your files) or to hand a colleague a copy of your setup. Restoring never overwrites anything: it creates a <em>new</em> drive from the snapshot and you create a workspace on that.</p>
<p>Git is still your real backup for code; snapshots are for everything else on the drive (and for the "I just deleted the wrong folder" moment).</p>`,
      links: [["Kubernetes: Volume Snapshots", "https://kubernetes.io/docs/concepts/storage/volume-snapshots/"]],
    },
    "shared-drive": {
      title: "Shared drives",
      short: "One folder, mounted by several workspaces at once.",
      body: `<p>A shared drive is a folder that more than one workspace can mount at the same time, under ${code("/shared/&lt;name&gt;")}. Home drives cannot do that (a disk belongs to one computer at a time); shared drives use a file service instead — Azure Files, NFS, EFS, Filestore — which many computers can open together, like a shared network folder.</p>
<p>Typical uses: data sets, build caches, a team's test fixtures. You create one on the Drives page, tick it in the Size step of any workspace, and optionally share it with teammates (read-write or read-only). Behind the scenes the portal gives the teammate's namespace a volume that points at the very same file share.</p>
<p>Not for: a developer's own home — that stays a fast private disk.</p>`,
      links: [["Azure Files CSI driver", "https://learn.microsoft.com/azure/aks/azure-files-csi"]],
    },
    share: {
      title: "Sharing a workspace",
      short: "Let a teammate SSH into your workspace.",
      body: `<p>Sharing a workspace adds a teammate's SSH key to its ${code("authorized_keys")} and gives them just enough Kubernetes permission to open the SSH tunnel to that one pod. They log in as ${code("dev")}, exactly like you: same files, same tools, same terminal. Great for pair programming and "can you look at this?" moments.</p>
<p>They connect with ${code("wsctl ssh you/&lt;name&gt;")} (or ${code("workspace ssh you/&lt;name&gt;")}), and the workspace shows up under <em>Shared with me</em>. Revoke any time; the key disappears within a minute.</p>
<p>Sharing is the one thing the portal does with its own identity (it must read the teammate's profile key and write a permission into your namespace) — and it is confined by an admission rule to exactly those objects.</p>`,
      links: [["Kubernetes RBAC: RoleBinding", "https://kubernetes.io/docs/reference/access-authn-authz/rbac/"]],
    },
    link: {
      title: "Links between workspaces",
      short: "Letting two developers' workspaces talk over the network.",
      body: `<p>Every workspace has a DNS name like ${code("ws-api.ws-alice.svc.cluster.local")}. Your <em>own</em> workspaces can already reach each other on any port — nothing to do. Workspaces of <em>different</em> developers are walled off by default (a NetworkPolicy per namespace).</p>
<p>A <strong>link</strong> opens a door in that wall for one pair of workspaces, optionally on chosen ports. Both sides must link — you to theirs and they to yours — so nobody can connect to your machine without your say-so. Until the other side links back the link shows <em>waiting</em>.</p>
<p>For a whole team working on one system, a <em>project namespace</em> is simpler: everyone's workspaces live together and talk freely. ${t("project", "What is a project?")}</p>`,
      links: [["Kubernetes Network Policies", "https://kubernetes.io/docs/concepts/services-networking/network-policies/"], ["Cilium network policy", "https://docs.cilium.io/en/stable/security/policy/"]],
    },
    project: {
      title: "Project namespaces",
      short: "A shared room where a team's workspaces live together.",
      body: `<p>Normally each developer has a private namespace (${code("ws-alice")}). A <strong>project</strong> is an extra namespace (${code("ws-proj-checkout")}) that several developers are members of. Workspaces created there are visible to every member and can talk to each other directly — a "virtual namespace" for the team's microservices, test rigs, or a hackathon.</p>
<p>A platform admin creates it (${code("workspace admin project create checkout --members alice,bob")} or Helm values). You choose it in the Basics step. Quotas apply to the project as a whole.</p>
<p>Optional extras for projects that need identity and routing between services: a service mesh (Istio ambient) — see the networking guide.</p>`,
      links: [["Kubernetes namespaces", "https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/"]],
    },
    wsctl: {
      title: "wsctl — the portal's command line",
      short: "A small Go program that uses this portal's API; no kubectl needed.",
      body: `<p>${code("wsctl")} does what this website does, from a terminal, and it talks only to the portal's HTTP API. You sign in once (${code("wsctl login")} — on Azure a device-code login with your company account), after which ${code("wsctl create")}, ${code("wsctl ssh")}, ${code("wsctl drives list")}, ${code("wsctl share add")} and friends work from any laptop with no cluster credentials at all.</p>
<p>${code("wsctl ssh")} opens a tunnel <em>through the portal</em> to your workspace's SSH server (${code("wsctl proxy")} is the OpenSSH ProxyCommand it installs), so VS Code Remote-SSH works too.</p>
<p>The older ${code("workspace")} command (Python) talks to Kubernetes directly with your kubeconfig; admins and GitOps pipelines use it. Both produce identical workspaces.</p>`,
      links: [["OAuth 2.0 device authorization grant", "https://learn.microsoft.com/entra/identity-platform/v2-oauth2-device-code"]],
    },
  };

  /* ------------------------------------------------------------------ guides */
  const guides = [
    {
      id: "start", icon: "🏁", minutes: 3, level: "Start here",
      title: "What is a workspace, and why would I want one?",
      summary: "The big picture in plain words, before you click anything.",
      sections: [
        { h: "The one-paragraph version", body: `<p>A ${t("workspace")} is your own Linux computer that lives in the company cloud. It already has the programming languages and tools you choose, it keeps your files between sessions, and you work on it from VS Code or a terminal exactly as if it were under your desk — except it is faster, it never needs reinstalling, and if you break it you can throw it away and make a new one in a minute.</p>` },
        { h: "Why not just use my laptop?", body: `<ul><li><strong>No setup days.</strong> New team member? New language? Pick it from a list; it is ready before your coffee is.</li><li><strong>Same for everyone.</strong> "Works on my machine" stops being a thing — everybody's workspace comes from the same ${t("image")}.</li><li><strong>Big when you need it.</strong> Need 16 CPUs and 64 GB for a build? Ask for it. Done? Stop it and the resources go back to the pool.</li><li><strong>Hardware you do not have.</strong> GPUs, serial devices, multicast networks and lab machines are a checkbox away.</li><li><strong>Safe.</strong> Your workspace is sealed off from other people's, and secrets never sit on your laptop.</li></ul>` },
        { h: "The three things to remember", body: `<ol><li><strong>Your files live on your ${t("volume", "home volume")}</strong>; the rest of the machine is rebuilt from the image every start. So keep work under ${code("/home/dev")} (that is ${code("~")}) — ${code("/workspace")} is a shortcut into it.</li><li><strong>It stops by itself</strong> after a few idle hours (${t("idle", "idle timeout")}) and starts again when you connect.</li><li><strong>You connect with ${t("ssh", "SSH")}</strong>, which needs an ${t("sshkey", "SSH key")} — one small setup step you do once.</li></ol>` },
        { h: "What you need before you start", body: `<ul><li>A laptop with VS Code (or any terminal).</li><li>An SSH key — the next guide shows how (5 minutes).</li><li>Your account must be <em>onboarded</em> by a platform admin. The <a href="#/home">Home</a> page tells you if it is.</li></ul>${tip("Ready? Continue with <a href=\"#/guides/ssh-key\">Make an SSH key</a>, then <a href=\"#/guides/first-workspace\">Create your first workspace</a>.")}` },
      ],
    },
    {
      id: "ssh-key", icon: "🔑", minutes: 5, level: "Setup",
      title: "Make an SSH key (your digital house key)",
      summary: "One-time setup. Without it nobody can log in to your workspace — not even you.",
      sections: [
        { h: "What you are making", body: `<p>An ${t("sshkey", "SSH key")} is a pair of files. The <strong>private key</strong> stays on your laptop and never leaves it. The <strong>public key</strong> is a single line of text that you give to the workspace. When you connect, the two halves check each other, and you are in — no password typed, nothing to steal.</p>` },
        { h: "Step 1 — Open a terminal", body: `<ul><li><strong>macOS:</strong> Applications → Utilities → Terminal.</li><li><strong>Windows:</strong> Start → <em>Windows Terminal</em> or <em>PowerShell</em>. (Windows 10/11 include OpenSSH.)</li><li><strong>Linux:</strong> you know where it is.</li></ul>` },
        { h: "Step 2 — Check if you already have one", body: cmd("ls ~/.ssh/id_ed25519.pub") + `<p>If a file name is printed, you already have a key — skip to step 4. If it says <em>No such file</em>, continue.</p>` },
        { h: "Step 3 — Make the key", body: cmd("ssh-keygen -t ed25519 -C \"you@example.com\"") + `<p>Press <kbd>Enter</kbd> to accept the default file name. When asked for a <em>passphrase</em>, choose one you can remember (it protects the private key if your laptop is lost). Type it twice.</p>${note("<strong>ed25519</strong> is the modern, short, fast key type. The older <code>rsa</code> type also works but the key is much longer.")}` },
        { h: "Step 4 — Copy the public key", body: cmd("cat ~/.ssh/id_ed25519.pub") + `<p>You will see one line that starts with ${code("ssh-ed25519 AAAA…")} and ends with your e-mail. Select the whole line and copy it.</p>${warn("Copy the file ending in <strong>.pub</strong>. The other file (without .pub) is the private key: never paste that anywhere.")}` },
        { h: "Step 5 — Save it in your profile", body: `<p>Open your <a href="#/profile">Profile</a> page in this portal, paste the line into <em>SSH public keys</em> and press <em>Save</em>. Every workspace you create from now on will accept this key. (You can add a second laptop's key later the same way.)</p>` },
        { h: "What you should see", body: `<p>The Profile page lists your key with a <em>fingerprint</em> such as ${code("SHA256:k3…")}. The fingerprint is a short nickname for the key; you can compare it with ${code("ssh-keygen -lf ~/.ssh/id_ed25519.pub")}.</p>${tip("Done! Next: <a href=\"#/guides/first-workspace\">Create your first workspace</a>.")}` },
      ],
    },
    {
      id: "first-workspace", icon: "🚀", minutes: 10, level: "Beginner",
      title: "Create your first workspace",
      summary: "From an empty page to a running Linux machine with your code in it.",
      sections: [
        { h: "Before you start", body: `<ul><li>Your SSH public key is saved in your <a href="#/profile">Profile</a> (<a href="#/guides/ssh-key">guide</a>).</li><li>You know which language(s) you want. Not sure? Pick one; you can always add more later.</li><li>Optional: the Git URL of the code you want to work on, for example ${code("git@github.com:acme/engine.git")}.</li></ul>` },
        { h: "Step 1 — Open the Create page", body: `<p>Click <a href="#/create"><strong>Create workspace</strong></a> in the menu. You will walk through a few short steps; nothing happens until the last one.</p>` },
        { h: "Step 2 — Name and languages", body: `<p>Give the workspace a short name (letters, numbers, dashes), like ${code("engine")} or ${code("hw-lab")}. Then tick one or more languages.</p><p>The <strong>first language you tick becomes the main one</strong>: it decides which ${t("image")} the workspace starts from. Extra languages are installed on top while the workspace boots. You can click <em>Make main</em> on any ticked language to change the order.</p>${note("Example: ticking <strong>Go</strong> then <strong>Python</strong> starts from the Go image and adds Python. Both work the same afterwards; starting from the language you use most just makes the first boot a little faster.")}` },
        { h: "Step 3 — Tools", body: `<p>Tick the tools you want. Some are built into the images (CMake, GDB…); others are downloaded from ${t("artifactory", "Artifactory")} when the workspace boots (ripgrep, gh, k9s…). Each card has an <em>(i)</em> with the tool's official documentation. Not sure? Skip this step — you can always ${code("sudo apt install")} inside the workspace or add tools later.</p>` },
        { h: "Step 4 — Code", body: `<p>Paste the repository URL to clone it on the first start. Prefer the <strong>SSH form</strong> (${code("git@github.com:org/repo.git")}) so you can push later.</p><p>Leave <em>How should Git log in?</em> on <strong>Deploy key</strong>. After creating, the portal shows you a public key to add to the repository (<a href="#/guides/git">Git guide</a>). Fill in your name and e-mail for commits if they are not already in your profile.</p>` },
        { h: "Step 5 — Size", body: `<p><em>Medium</em> (4 CPU, 8 GB memory, 30 GB disk) is right for most projects. Pick <em>Large</em> for Java/Android/C++ builds. The <strong>idle timeout</strong> is how long the workspace waits with nobody connected before switching itself off (default 4 hours).</p>` },
        { h: "Step 6 — Advanced (usually skip)", body: `<p>This is where multicast networking, hardware profiles, devices and a public SSH address live. First workspace? Leave everything as it is.</p>` },
        { h: "Step 7 — Review and create", body: `<p>Check the summary and press <strong>Create workspace</strong>. A progress panel shows the workspace going from <em>Pending</em> to <em>Starting</em> to <em>Running</em>. The first start takes about a minute: the disk is created, the image is unpacked, and your tools and code are fetched. Later starts are faster.</p>` },
        { h: "What you should see", body: `<p>A green <strong>Running</strong> badge and a <em>Connect</em> panel with three options. Continue with <a href="#/guides/connect-vscode">Connect VS Code</a>.</p>${warn("If the workspace stays <em>Pending</em> for more than two minutes, open its page and read the <em>Boot log</em>; the most common reasons are a full quota or a typo in the repository URL. See <a href=\"#/guides/troubleshooting\">Troubleshooting</a>.")}` },
      ],
    },
    {
      id: "connect-vscode", icon: "🧩", minutes: 7, level: "Beginner",
      title: "Connect VS Code to your workspace",
      summary: "Edit, run and debug in the workspace from the editor on your laptop.",
      sections: [
        { h: "How it works", body: `<p>VS Code splits in two: the window on your laptop, and a small <em>VS Code server</em> inside the workspace that does the real work. They talk over ${t("ssh", "SSH")}. Because the workspace is not on the internet, the SSH connection travels inside your Kubernetes login (${t("proxycommand", "how?")}). The ${t("cli", "workspace command")} writes that configuration for you — once per workspace.</p>` },
        { h: "Step 1 — Install two small things on your laptop", body: `<ol><li>The <strong>Remote - SSH</strong> extension in VS Code (Extensions panel → search <em>Remote - SSH</em> → Install).</li><li>The ${code("workspace")} command and ${code("kubectl")}, if you do not have them yet (ask in the help channel; both are single files).</li></ol>` },
        { h: "Step 2 — Write the SSH configuration", body: `<p>In a terminal on your laptop:</p>${cmd("workspace ssh-config engine")}<p>(replace ${code("engine")} with your workspace name). This adds a block to ${code("~/.ssh/config")} named ${code("ws-engine")}, registers this laptop's key with the workspace, and pins the workspace's ${t("hostkey", "host key")}. The workspace page also shows the block if you prefer to paste it by hand.</p>` },
        { h: "Step 3 — Raise one VS Code setting", body: `<p>A stopped workspace needs up to a minute to start when you connect. VS Code gives up after 15 seconds unless you tell it to wait: open <em>Settings</em>, search <em>remote.SSH.connectTimeout</em> and set it to <strong>180</strong>.</p>` },
        { h: "Step 4 — Connect", body: `<p>Press <kbd>F1</kbd>, type <em>Remote-SSH: Connect to Host…</em>, choose ${code("ws-engine")}. The first time, VS Code installs its server in the workspace (30–60 seconds; it is kept on your volume, so next time is instant). Then <em>File → Open Folder</em> → ${code("/workspace")}.</p><p>Shortcut from a terminal: ${code("workspace code engine")} or ${code("code --remote ssh-remote+ws-engine /workspace")}.</p>` },
        { h: "What you should see", body: `<p>The green box in the bottom-left corner of VS Code says <strong>SSH: ws-engine</strong>. The integrated terminal (<kbd>Ctrl</kbd>+<kbd>\`</kbd>) is a shell <em>inside</em> the workspace: try ${code("uname -a")} and ${code("echo $WORKSPACE_NAME")}.</p>${tip("Ports panel: when a program in the workspace listens on a port, VS Code offers to forward it, so <code>http://localhost:8080</code> on your laptop reaches it (" + t("portforward", "port forwarding") + ").")}` },
        { h: "Other editors", body: `<p>Anything that speaks SSH works with the same ${code("ws-engine")} host: JetBrains Gateway, Zed, Neovim, Emacs TRAMP, or plain ${code("ssh ws-engine")}.</p>` },
      ],
    },
    {
      id: "connect-terminal", icon: "⌨️", minutes: 4, level: "Beginner",
      title: "Use the terminal and the workspace command",
      summary: "Everything the portal does, from the command line.",
      sections: [
        { h: "Connect", body: cmd("workspace ssh engine        # starts it if it was stopped\nssh ws-engine               # the same, after ssh-config") + `<p>You land in ${code("/home/dev")} as user ${code("dev")}. ${code("sudo")} works without a password (${t("root", "why that is safe")}).</p>` },
        { h: "Everyday commands", body: cmd("workspace list\nworkspace status engine\nworkspace stop engine\nworkspace start engine\nworkspace idle engine 8h            # or: off\nworkspace logs engine -f            # boot and tool-install log\nworkspace port-forward engine 8080:8080\nworkspace delete engine             # keeps the disk; add --purge to remove it") },
        { h: "Create from the terminal", body: cmd("workspace catalog\nworkspace create api --lang go,python --tools ripgrep,kubectl --repo git@github.com:acme/api.git\nworkspace create fw --lang cpp --profile hardware --device smarter-devices/ttyUSB0=1 --multicast") + `<p>Portal and CLI create exactly the same objects (${t("yaml", "what objects?")}), so a workspace made in one can be managed from the other.</p>` },
        { h: "Inside the workspace", body: `<ul><li>${code("ws-logs")} — the boot log (what was installed, what failed).</li><li>${code("~/README-workspace.md")} — a short reminder of where things are.</li><li>${code("echo $WORKSPACE_LANGUAGES $WORKSPACE_TOOLS")} — what this workspace was created with.</li><li>${code("tmux")} — keep long jobs running after you disconnect.</li></ul>` },
      ],
    },
    {
      id: "files-and-saving", icon: "💾", minutes: 4, level: "Important",
      title: "Where your files live, and what survives a restart",
      summary: "The single most useful thing to understand about workspaces.",
      sections: [
        { h: "Two kinds of places", body: `<table class="tbl"><thead><tr><th>Place</th><th>Survives stop/start?</th><th>Examples</th></tr></thead><tbody><tr><td>${code("/home/dev")} (your ${t("volume", "home volume")})</td><td><strong>Yes</strong></td><td>code in ${code("~/workspace")}, shell history, ${code("~/.cargo")}, ${code("~/go")}, ${code("~/.npm")}, Python venvs, VS Code server &amp; extensions, downloaded .deb files</td></tr><tr><td>Everything else (the image)</td><td><strong>No</strong> — rebuilt from the image</td><td>${code("sudo apt install foo")}, files in ${code("/opt")} or ${code("/tmp")}, changes to ${code("/etc")}</td></tr></tbody></table>` },
        { h: "So where do I put things?", body: `<ul><li><strong>Code:</strong> ${code("~/workspace/&lt;repo&gt;")} (${code("/workspace")} points there).</li><li><strong>Tools you need every time:</strong> tick them in the Tools step (they reinstall themselves at boot, quickly, from the cached downloads on your volume), or ask the platform team to add them to the catalog.</li><li><strong>Personal settings:</strong> edit ${code("~/.bashrc")}, ${code("~/.gitconfig")} — they are on the volume. Or use a ${t("dotfiles")} repository.</li><li><strong>Language toolchains:</strong> ${code("rustup toolchain add nightly")}, ${code("cargo install")}, ${code("go install")}, ${code("npm i -g")}, ${code("pipx install")} all land in your home folder and persist.</li></ul>` },
        { h: "Running out of space?", body: `<p>The workspace page shows the disk size. Ask a platform admin to grow it (most storage classes can grow a volume without data loss), or clean caches: ${code("cargo clean")}, ${code("go clean -cache")}, ${code("npm cache clean --force")}, ${code("rm -rf ~/.cache/*")}.</p>` },
        { h: "Backups", body: `<p>Push your code to Git often — that is the real backup. The platform may also snapshot volumes, but Git is yours and works everywhere.</p>` },
      ],
    },
    {
      id: "git", icon: "🌿", minutes: 8, level: "Beginner",
      title: "Git: get code, save your work, share it",
      summary: "Cloning on first boot, the deploy key, and the daily commit–push loop.",
      sections: [
        { h: "Cloning on first boot", body: `<p>If you gave a repository URL when creating the workspace, the code is already in ${code("~/workspace/&lt;repo-name&gt;")}. If the clone failed (for example the deploy key was not yet added), the <em>Boot log</em> on the workspace page says so; fix the key and run the clone by hand:</p>${cmd("cd ~/workspace && git clone git@github.com:acme/engine.git")}` },
        { h: "Adding the deploy key (once per repository)", body: `<ol><li>Open the workspace page; copy the <strong>Deploy key</strong> (one line starting with ${code("ssh-ed25519")}).</li><li>On GitHub: repository → <em>Settings</em> → <em>Deploy keys</em> → <em>Add deploy key</em>; paste; tick <strong>Allow write access</strong>. On GitLab: <em>Settings → Repository → Deploy keys</em>, grant write. (Or add it under your account's SSH keys to use it for all your repos.)</li></ol><p>Why a separate key? See ${t("deploykey", "deploy key")}. Prefer HTTPS? Choose <em>Access token</em> instead (${t("token", "what is a token?")}).</p>` },
        { h: "Your name on commits", body: `<p>Set once (saved on your volume):</p>${cmd("git config --global user.name \"Ada Lovelace\"\ngit config --global user.email \"ada@example.com\"")}<p>The portal does this for you if your Profile has a name and e-mail.</p>` },
        { h: "The daily loop", body: cmd("git pull                       # get teammates' changes\n# …edit files…\ngit status                     # what changed?\ngit add -A                     # stage everything\ngit commit -m \"Fix the parser\"  # save a step with a message\ngit push                       # send it to the server") + `<p>Working on a feature? Make a branch first: ${code("git switch -c my-feature")}, push with ${code("git push -u origin my-feature")}, then open a pull request on the web.</p>` },
        { h: "Useful extras", body: `<ul><li>The <strong>gh</strong> tool (GitHub CLI) lets you open pull requests from the terminal: ${code("gh pr create")}.</li><li><strong>lazygit</strong> is a friendly terminal UI for git.</li><li>VS Code's <em>Source Control</em> panel does commit/push with clicks.</li></ul>` },
      ],
    },
    {
      id: "languages-tools", icon: "🧰", minutes: 5, level: "Beginner",
      title: "Languages and tools: mixing Go, Python and more",
      summary: "How multi-language workspaces work and how to add tools later.",
      sections: [
        { h: "One workspace, several languages", body: `<p>Tick as many languages as you like. The first one picks the base ${t("image")} (for example ${code("workspace-go")}); the others are installed as <em>features</em> during the first boot and then cached on your volume. Everything ends up in the usual places: ${code("python3")}, ${code("go")}, ${code("cargo")}, ${code("node")}, ${code("java")} are all on your ${code("PATH")}.</p>${note("Teams that often need the same mix can ask the platform team for a pre-baked image (for example <code>workspace-go-python</code>) — it then shows up in the image list and boots faster.")}` },
        { h: "Where tools come from", body: `<ul><li><strong>In the image</strong> — compilers, git, CMake, GDB and friends are part of the image.</li><li><strong>Feature scripts</strong> — tools like kubectl, docker CLI or protobuf run a small install script at boot.</li><li><strong>Artifactory downloads</strong> — ripgrep, gh, k9s, shellcheck… are single files fetched from ${t("artifactory", "Artifactory")}. The Tools page shows whether each one is available right now and which version.</li></ul>` },
        { h: "Adding a tool to an existing workspace", body: `<p>Open the workspace in the portal → <em>Recreate with changes</em> (this keeps your disk), tick the tool, create. Or inside the workspace: ${code("sudo apt install &lt;pkg&gt;")} for a quick one-off (remember: gone after a restart unless it is in your tool list).</p>` },
        { h: "Asking for a new language or tool", body: `<p>The catalog is a single JSON file that platform admins maintain. A new downloadable tool is a five-line entry pointing at a file in Artifactory; a new language is a short install script. Open a request in the platform team's queue with the official download URL and what the tool is for.</p>` },
      ],
    },
    {
      id: "artifactory", icon: "📦", minutes: 5, level: "Background",
      title: "Artifactory: our safe download store",
      summary: "Why nothing in a workspace comes from the open internet, and what that means for you.",
      sections: [
        { h: "What it is", body: `<p>${t("artifactory", "Artifactory")} is a server that stores software packages and keeps copies of packages from the internet. Think of it as the school library: instead of every student buying every book, the library has one copy that everybody borrows — and the librarian checks that the book is the real thing.</p>` },
        { h: "What it does for you", body: `<ul><li><strong>Speed:</strong> the second download of anything is served from inside the company network.</li><li><strong>Safety:</strong> packages are scanned, and a package that disappears or is tampered with upstream is still safe in the cache.</li><li><strong>Works offline:</strong> workspaces in clusters without internet access still get everything.</li><li><strong>One place to look:</strong> the <a href="#/tools">Tools</a> page lists what is available and which versions.</li></ul>` },
        { h: "How workspaces use it", body: `<p>At boot the workspace configures every ${t("packagemanager", "package manager")} to use Artifactory: ${code("pip")}/${code("uv")}, ${code("npm")}/${code("pnpm")}/${code("yarn")}, ${code("go")}, ${code("cargo")}, ${code("mvn")}/${code("gradle")} and ${code("apt")}. You use them exactly as before; nothing to type differently. The workspace image itself and the selected tools are downloaded from it too.</p>` },
        { h: "If a download is refused", body: `<p>Some repositories require you to be logged in. Save a personal Artifactory token in your <a href="#/profile">Profile</a> (<em>User profile → Generate identity token</em> in Artifactory). The workspace picks it up at the next start. Never paste tokens into code or chat.</p>` },
      ],
    },
    {
      id: "stop-start-cost", icon: "⏯️", minutes: 3, level: "Beginner",
      title: "Stop, start, idle timeout and delete",
      summary: "What each button does to your files and to the shared cluster.",
      sections: [
        { h: "Stop", body: `<p>Removes the running ${t("pod")}. Your ${t("volume", "disk")} stays. Takes a few seconds. Use it when you are done for the day — it frees CPU and memory for colleagues.</p>` },
        { h: "Start", body: `<p>Creates a new pod and attaches your disk; about a minute. You rarely press it: connecting with SSH or VS Code starts a stopped workspace automatically.</p>` },
        { h: "Idle timeout", body: `<p>The workspace stops itself when nobody has been connected for this long (${t("idle", "details")}). Change it on the workspace page. Long job tonight? Set <em>off</em>, and remember to set it back.</p>` },
        { h: "Delete", body: `<p>Removes the workspace definition and its SSH/Git secrets. <strong>By default your disk is kept</strong> so a new workspace with the same name gets your files back. Tick <em>Also delete my files</em> only when you really want everything gone — there is no undo.</p>` },
        { h: "Recreate with changes", body: `<p>Changing languages, tools, size or profile means replacing the pod with a new one; the disk is kept. It is the same as creating a workspace with the same name again.</p>` },
      ],
    },
    {
      id: "multicast", icon: "📡", minutes: 8, level: "Advanced",
      title: "Testing multicast: one sender, many listeners",
      summary: "Turn on the multicast network and prove it works between two workspaces.",
      sections: [
        { h: "What multicast is", body: `<p>${t("multicast", "Multicast")} is radio for networks: one packet is sent to a group address and every machine that joined the group hears it. Normal cloud networks do not carry it, so workspaces that need it get a second network interface, ${code("net1")}, attached to a network that does.</p>` },
        { h: "Step 1 — Create the workspace with multicast", body: `<p>In the <em>Advanced</em> step switch on <strong>Multicast network</strong> and in <em>Tools</em> tick <strong>mcast</strong> (test tools). Create. Inside the workspace you will see the extra interface:</p>${cmd("ip -4 addr show net1\nip route show 224.0.0.0/4     # multicast is routed out of net1")}` },
        { h: "Step 2 — Prove it on one workspace", body: cmd("mcast-check") + `<p>It joins ${code("239.255.0.42")}, sends a packet and reports <em>multicast OK</em> when it receives its own packet back on ${code("net1")}.</p>` },
        { h: "Step 3 — Prove it between two workspaces", body: `<p>Create a second multicast workspace (or ask a teammate). In one run ${code("mcast-check --listen")}; in the other run ${code("mcast-check")}. The listener prints the sender's packet.</p>${cmd("# workspace A\nmcast-check --listen\n# workspace B\nmcast-check --group 239.255.0.42 --port 5007")}` },
        { h: "Writing your own code", body: `<p>Bind the receiving socket to the port, join the group with ${code("IP_ADD_MEMBERSHIP")} using ${code("net1")}'s address, and on the sending side set ${code("IP_MULTICAST_IF")} to that address and a TTL &gt; 1. The ${code("mcast-check")} script is a 60-line Python example you can copy: ${code("cat $(which mcast-check)")}. For load tests use ${code("iperf -u -c 239.255.0.42 -T 8")} / ${code("iperf -s -u -B 239.255.0.42")}.</p>` },
        { h: "If nothing arrives", body: `<ul><li>Is your program bound to ${code("net1")} (not ${code("eth0")})? ${code("WORKSPACE_MCAST_IFACE")} tells you the name.</li><li>Firewalls inside the workspace? None by default — check with ${code("sudo iptables -S")}.</li><li>Across servers it depends on the physical switch having an IGMP querier; same-server delivery always works. The platform page lists which node pool carries the multicast network.</li><li>${code("sudo tcpdump -ni net1 udp port 5007")} shows whether packets reach the interface at all.</li></ul>` },
      ],
    },
    {
      id: "hardware", icon: "🔌", minutes: 10, level: "Advanced",
      title: "Hardware and device-driver development (lab tier)",
      summary: "Profiles, devices, host paths, and the safety rules around them.",
      sections: [
        { h: "Why there is a lab tier", body: `<p>A ${t("driver", "device driver")} runs inside the ${t("kernel")}. A bug there can freeze the whole server, so driver work does not run on the shared servers. The <strong>lab tier</strong> is a separate pool of servers where your workspace can be ${t("privileged")} and where at most one such workspace runs per server. Your account needs lab access (the Home page says <em>Lab: yes</em>); ask the platform team if it says <em>no</em>.</p>` },
        { h: "Pick a profile", body: `<table class="tbl"><thead><tr><th>Profile</th><th>Kernel</th><th>Use it for</th></tr></thead><tbody><tr><td><strong>Hardware lab</strong></td><td>the server's kernel</td><td>USB/serial/I2C/camera drivers in user space, out-of-tree modules against the server kernel, GPU and eBPF work, perf tracing</td></tr><tr><td><strong>Kernel lab (micro-VM)</strong></td><td>your own kernel inside a small VM (${t("sandbox", "Kata")})</td><td>modules you expect to crash, PCI devices handed over with VFIO</td></tr></tbody></table>` },
        { h: "Step 1 — Create the workspace", body: `<p>Create → <em>Advanced</em> → Profile: <strong>Hardware lab</strong>. Tick the <strong>devices</strong> you need (${t("deviceplugin", "what are these?")}) — a USB serial adapter, a GPU, hugepages. Add extra host paths only if a device is not covered by a plugin (for example ${code("/sys/bus/pci")}). The hardware profile already mounts ${code("/lib/modules")}, ${code("/usr/src")} and ${code("/lib/firmware")} read-only so module builds find the kernel headers.</p>` },
        { h: "Step 2 — Build a module against the host kernel", body: cmd("uname -r                                  # the server kernel version\nls /lib/modules/$(uname -r)/build         # headers are there\nmake -C /lib/modules/$(uname -r)/build M=$PWD modules\nsudo insmod ./hello.ko && dmesg | tail") + warn("In the <strong>Hardware lab</strong> profile <code>insmod</code> loads your module into the real server kernel. A bad module can take the server down — only your workspace is on it, but save your work first. For risky modules use the <strong>Kernel lab</strong> profile.") },
        { h: "Step 3 — Talk to a device", body: cmd("ls -l /dev/ttyUSB0                        # from the smarter-devices/ttyUSB0 request\nsudo apt install -y minicom && minicom -D /dev/ttyUSB0 -b 115200\nnvidia-smi                                 # if you asked for nvidia.com/gpu\nlspci -nnk | grep -A3 -i ethernet           # PCI devices and their drivers") },
        { h: "What is still not allowed, and why", body: `<ul><li><strong>No host network / host PID / host IPC</strong> — those would let a workspace watch or disturb other pods on the server. Use SR-IOV VFs for network cards.</li><li><strong>Host paths only under</strong> ${code("/dev")}, ${code("/sys")}, ${code("/lib/modules")}, ${code("/usr/src")}, ${code("/lib/firmware")}, ${code("/run/udev")} — never the cluster's own files.</li><li><strong>Lab servers only</strong> — the hardware profile cannot land on the shared pool.</li></ul><p>These rules are enforced by the cluster, not just by the portal.</p>` },
      ],
    },
    {
      id: "drives", icon: "🗄️", minutes: 6, level: "Important",
      title: "Your files, drives and snapshots",
      summary: "Why deleting a workspace does not delete your work, and how to get it back.",
      sections: [
        { h: "The locker and the desk", body: `<p>A ${t("workspace")} is a desk that is set up fresh every time; your ${t("drive", "home drive")} is the locker. The desk can be stopped, deleted, replaced with a bigger one, or crash — the locker is untouched. This guide is about the locker.</p>` },
        { h: "What happens when…", body: `<table class="tbl"><thead><tr><th>You…</th><th>The workspace</th><th>Your drive</th></tr></thead><tbody><tr><td>stop</td><td>pod removed, nothing billed for CPU/RAM</td><td>kept</td></tr><tr><td>delete</td><td>gone</td><td>kept, shown as <em>detached</em> on the Drives page</td></tr><tr><td>recreate with changes</td><td>replaced</td><td>kept, same drive</td></tr><tr><td>have a pod crash / node failure</td><td>restarted by Kubernetes</td><td>re-attached automatically</td></tr><tr><td>Delete drive / ${code("--purge")}</td><td>—</td><td><strong>erased</strong> (the only way)</td></tr></tbody></table>${tip("There is even a guard on the cluster: deleting a home drive is refused unless it was explicitly marked for deletion, so a mistyped kubectl command cannot do it.")}` },
        { h: "Getting a detached drive back", body: `<ol><li>Open <strong>Drives</strong>. Detached drives show who used them last and which languages.</li><li>Click <em>New workspace on it</em>. The Create wizard opens with that drive preselected (Size step → <em>Start from</em>).</li><li>Pick any languages and tools — they do not have to match — and create. Your files are exactly where you left them.</li></ol>${cmd("wsctl drives list\nwsctl create api --lang go --from-drive ws-old-home")}` },
        { h: "Snapshots", body: `<p>A ${t("snapshot")} is a frozen copy. Take one before risky changes; restore by creating a <em>new</em> workspace from it (the original is never overwritten).</p>${cmd("wsctl drives snapshot api --name before-upgrade\nwsctl drives snapshots api\nwsctl create api2 --lang go --from-snapshot before-upgrade")}` },
        { h: "Choosing the kind of disk", body: `<p>The Size step offers the storage choices your platform team configured — typically a fast SSD (default) and a cheaper standard disk. Disks can grow (Drives page → <em>Grow</em>) but never shrink.</p>` },
        { h: "Shared folders", body: `<p>Need several workspaces — or several people — to see the same files at once? That is a ${t("shared-drive", "shared drive")}: create it on the Drives page, tick it in the Size step, it appears at ${code("/shared/&lt;name&gt;")}. Share it with teammates (read-write or read-only) from the same page.</p>` },
        { h: "Still: push to Git", body: `${warn("Drives and snapshots protect you from accidents on the platform. They are not a substitute for pushing your code: Git works even when the cluster does not.")}` },
      ],
    },
    {
      id: "networking", icon: "🔗", minutes: 7, level: "Intermediate",
      title: "How workspaces talk to each other",
      summary: "Run a front end in one workspace and its API in another — yours or a teammate's.",
      sections: [
        { h: "Every workspace has a name", body: `<p>Inside the cluster your workspace is reachable as ${code("ws-&lt;name&gt;.&lt;namespace&gt;.svc.cluster.local")} (the workspace page's <em>Network</em> box shows it, and ${code("$WORKSPACE_DNS")} holds it inside the workspace). From another workspace in the same namespace the short form ${code("ws-&lt;name&gt;")} works too.</p>` },
        { h: "Tier 1 — your own workspaces: already connected", body: `<p>All workspaces in your namespace can talk on every port. Start a server in one and call it from the other:</p>${cmd("# in workspace api\npython3 -m http.server 8000\n# in workspace web\ncurl http://ws-api:8000/")}` },
        { h: "Tier 2 — a team: project namespace", body: `<p>For a team building one system, ask a platform admin for a ${t("project")}. Everyone's workspaces live in ${code("ws-proj-&lt;team&gt;")}, see each other in the list, and reach each other directly — the simplest "virtual namespace".</p>${cmd("workspace admin project create checkout --members alice@corp.example,bob@corp.example\nwsctl create api --lang go --project checkout")}` },
        { h: "Tier 3 — two developers, ad hoc: a link", body: `<p>Want your ${code("web")} to call Bob's ${code("api")} without a project? Create a ${t("link")} on your workspace page (or ${code("wsctl link add web bob@corp.example/api --port 8080")}). Bob does the same towards you. Until both sides agree the link shows <em>waiting</em>. Then:</p>${cmd("curl http://ws-api.ws-bob.svc.cluster.local:8080/")}${note("Links are plain Kubernetes NetworkPolicies in your own namespace — nothing magic, nothing an admin has to approve, and you can remove them any time.")}` },
        { h: "When to add a service mesh", body: `<p>The three tiers cover most needs with DNS + NetworkPolicy. A mesh (Istio in <em>ambient</em> mode) adds identity between workloads (mTLS), request-level rules (“only GET from web”) and header-based routing (“send requests with <code>x-dev: alice</code> to Alice's workspace instead of the team's shared API”). The platform team enables it per cluster; your workspaces need no change. Details and examples: ${code("docs/workspace-networking.md")} in the repository.</p>` },
        { h: "Multicast", body: `<p>Multicast (UDP to a group address) is separate from all this: tick <em>Multicast</em> in the Advanced step and use ${code("mcast-check")} inside — see the ${t("multicast")} guide.</p>` },
      ],
    },
    {
      id: "troubleshooting", icon: "🩺", minutes: 6, level: "Reference",
      title: "When something goes wrong",
      summary: "The usual suspects, in the order to check them.",
      sections: [
        { h: "The workspace stays Pending", body: `<ul><li><strong>Quota full</strong> — the workspace page shows <em>exceeded quota</em>. Stop or shrink another workspace.</li><li><strong>No disk yet</strong> — the first start creates the volume; on some storage this takes a minute.</li><li><strong>Lab pool busy</strong> — hardware workspaces need a free lab server (one workspace per server).</li><li><strong>Image not found</strong> — <em>ErrImagePull</em>: the tag you chose is not in Artifactory; recreate with the default tag.</li></ul>` },
        { h: "SSH or VS Code cannot connect", body: `<ol><li>Is the workspace <em>Running</em> in the portal? If it is <em>Stopped</em>, connecting should start it — wait up to a minute.</li><li>Does ${code("kubectl get pods -n ws-&lt;you&gt;")} work on your laptop? The SSH tunnel uses your Kubernetes login.</li><li>Did you run ${code("workspace ssh-config &lt;name&gt;")} on <em>this</em> laptop? Each laptop registers its own key.</li><li>VS Code: is ${code("remote.SSH.connectTimeout")} set to 180?</li><li><em>Permission denied (publickey)</em>: your key is not on the workspace — add it on the workspace page under <em>SSH keys</em>.</li><li><em>Host key changed</em>: only expected after an admin recreated the workspace from scratch; compare the fingerprint on the workspace page, then remove the old line from ${code("~/.config/workspace/known_hosts")}.</li></ol>` },
        { h: "A tool or language is missing", body: `<p>Read the <em>Boot log</em> on the workspace page (or ${code("ws-logs")} inside). <em>tool FAILED</em> lines name the tool and the log file. Usually Artifactory refused the download (token needed — see <a href="#/guides/artifactory">Artifactory</a>) or the catalog entry points at a version that no longer exists (tell the platform team).</p>` },
        { h: "Git cannot clone or push", body: `<ul><li><em>Permission denied (publickey)</em> from GitHub/GitLab: the deploy key is not added, or was added without write access.</li><li>HTTPS URL with a deploy key? Deploy keys only work with SSH URLs (${code("git@…")}). Switch the URL or use a token.</li><li>${code("ssh -T git@github.com")} inside the workspace tests the key directly.</li></ul>` },
        { h: "My files disappeared", body: `<p>Were they outside ${code("/home/dev")}? Then they were on the image side and are gone after a restart (<a href="#/guides/files-and-saving">why</a>). Files under your home folder are only removed by <em>Delete → also delete my files</em>.</p>` },
        { h: "Still stuck?", body: `<p>Copy the workspace name, the time, and the last 20 lines of the boot log into the help channel. Admins can see exactly the same objects you can, plus cluster events.</p>` },
      ],
    },
  ];

  return { topics, guides, t };
})();
