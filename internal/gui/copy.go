package gui

// Copy strings shown to users during onboarding. Kept here so the writing can
// be tuned without hunting through layout code.

// First-run setup: one screen. The writing carries most of the weight: this is
// the only place a user is told what P2Pool is, and that kind-miner will start
// with their computer.
const (
	OnboardWalletTitle = "Where should your rewards go?"
	OnboardWalletBody  = "kind-miner mines on P2Pool: a peer-to-peer pool with no operator, no account and no fee. Your machine works alongside other miners, and when the group finds a block the reward is split by the work each miner did and paid straight to the addresses written into the block itself. Nobody holds your coins, including us."
	OnboardWalletNote  = "Use a primary address — it starts with 4 and is 95 characters. Subaddresses and integrated addresses cannot receive P2Pool payouts, because the reward arrives as a coinbase output."

	// OnboardStartupLabel is the autostart disclosure. It is worded as what
	// will happen, not as a setting, because it is on unless unticked.
	OnboardStartupLabel = "Starts with your computer and mines gently in the background"

	OnboardMoreOptions = "More options"
	OnboardNodeNote    = "Our node is reached as a Tor hidden service, so the operator never learns your IP — but you are trusting our copy of the chain. Your own node is the private option; it wants around 180 GB of disk and a day or two to sync."
	OnboardMoreLater   = "Everything else — how much of the machine to use, the P2Pool sidechain, your own xmrig and p2pool — is in Settings, and has a sensible default until you look."

	OnboardStart = "Start mining"
)

// Shown once, when first-run setup finishes and the window tucks itself into
// the tray. Without it the window simply vanishes, which reads as a crash.
const (
	TrayNoticeTitle = "kind-miner is mining"
	TrayNoticeBody  = "It lives in your system tray now. Click the icon to see what it's doing, pause it, or change how much of the machine it takes."
)

// Wallet entry and its validation messages.
const (
	WalletPlaceholder = "4… or 8… (95 characters)"
	WalletHelp        = "Don't have a Monero wallet yet?"
	WalletHelpLink    = "Get one"
	WalletHelpURL     = "https://www.getmonero.org/downloads/"

	WalletErrEmpty      = "Address is required."
	WalletErrPrefix     = "Address must start with 4."
	WalletErrLength     = "Address should be 95 characters long."
	WalletErrCharset    = "Address contains invalid characters."
	WalletErrChecksum   = "This address has a typo — its checksum doesn't match. Copy it from your wallet again."
	WalletErrSubaddress = "That's a subaddress, which P2Pool can't pay. Use your primary address — it starts with 4."
	WalletErrIntegrated = "That's an integrated address, which P2Pool can't pay. Use your primary address."
	// Checked in full, checksum included, so "valid" is earned.
	WalletOK = "A valid Monero address."
)

// Setup-progress screen. Each component shown during first run gets a
// friendly explanation underneath so the user understands what is being
// installed and why.
const (
	SetupTitle = "Setting things up…"

	XMRigName  = "XMRig"
	XMRigBlurb = "The open-source program that quietly converts your computer's spare cycles into Monero. Crafted by a community of independent developers, free for anyone to use, and respectful of the machine it runs on."

	P2PoolName  = "p2pool"
	P2PoolBlurb = "A cooperative network where thousands of miners pool their computing power and share rewards fairly — with no fees, no operators, and no opportunity for anyone to cheat the math. A generous way to put your machine to honest work, alongside others doing the same the world over."

	TorName  = "Tor"
	TorBlurb = "A worldwide network of volunteer relays that wraps every connection in layers of encryption, like the rings of an onion. It conceals who is mining and where — preserving your privacy and keeping mining open to everyone, everywhere."

	NodeName  = "Monero node"
	NodeBlurb = "Your window onto the Monero network — a server that tells your miner what blocks have been found and which transactions are circulating. Reaching it through Tor means even the node operator never learns your identity."

	I2PName  = "I2P"
	I2PBlurb = "An anonymity network where every participant helps every other stay unseen. Your miner uses it to communicate with other miners discreetly, without revealing your location to anyone in the conversation."

	MinerName  = "Starting the miner"
	MinerBlurb = "Bringing all the pieces together and inviting your computer to begin earning. From here, kind-miner lives in your system tray — working when you don't need the machine, and stepping aside the moment you do."
)

// Warning shown when the active wireless link runs with 802.11 power save on.
//
// It names the remedy rather than only the problem: the failure it describes
// looks so much like a broken internet connection that a user will otherwise
// spend the evening restarting their router. See monitor.WiFiPowerSave.
// Values are short because kvList elides anything past valueWidth; the full
// explanation and the remedy go to the log.
const (
	WiFiPowerSaveKey = "Wi-Fi power save"
	WiFiPowerSaveOn  = "On — may stall this network"
	WiFiPowerSaveOff = "Off"
)

// hubModeNote explains the hub mode in the connection settings. It says whose
// wallet is paid, because that is the thing a household member pairing their
// laptop most needs to know before they do it.
const hubModeNote = "Choose mode \"hub\" to mine to another machine in the house — usually a Nodo running kind-minerd. Find a hub sets up a new one to pay the address on the Payout tab; for one already set up, paste the code its owner has here, or that kind-minerd pair prints on it. This computer then runs only the miner, and the hub pays its owner's wallet."

// Find a hub: the search, and setting a new hub up.
const (
	findHubButton    = "Find a hub on this network…"
	findHubTitle     = "Find a hub"
	findHubSearching = "Looking for hubs on this network…"
	findHubNone      = "No hub answered. Is kind-minerd running on it, on the same network as this computer? Give its address instead:"
	findHubSome      = "Hubs on this network:"
	findHubManual    = "or its address, e.g. 192.168.1.20"
	findHubLook      = "Look there"
	findHubSetUp     = "Set up…"
	// findHubIsSetUp is shown against a hub that already has an owner: only
	// its code can pair with it now, and the owner's desktop has it.
	findHubIsSetUp = "already set up — paste its pairing code from the desktop that set it up"
	findHubNotSet  = "not set up yet"
	// findHubNeedsAddress: setting a hub up is what asks for the address,
	// once, and the Payout tab is where this desktop already keeps it.
	findHubNeedsAddress = "Enter your Monero address on the Payout tab first: the hub will pay it for every device in the house."
	// findHubConfirm names the address and the certificate. This is the one
	// moment the desktop takes the hub's certificate on trust; from here on
	// it accepts that certificate only.
	findHubConfirm   = "Set %s up to mine for %s?\n\nEvery device that mines to it pays this address. This computer will trust it by its certificate, %s, and no other."
	findHubSettingUp = "Setting %s up…"
	findHubDone      = "%s is set up. Save to mine to it; it starts mining itself within a few minutes."
	findHubFailed    = "Could not set %s up: %s"
)

// Connection card labels for a machine paired with a hub.
const (
	hubConnected   = "Connected"
	hubUnreachable = "Hub unreachable"
	hubWrongCert   = "Not your hub"
)

// The dashboard while the user is at the machine: where it is mining and when
// that changes. Filled with the cores now, all the cores it will use, the
// preset, and (first form) the time left.
const (
	// Short enough for the header at 16-point text: the longer "… while
	// you're here · all of them at full Balanced in ~5m" made the dashboard
	// 809 points wide, in a 760-point window.
	statusGentleCoresCountdown = "Mining gently on %d of %d cores · all %d at %s in %s"
	statusGentleCores          = "Mining gently on %d of %d cores while you're here · %s"
)

// kindnessWhyNote sits under the kindness presets. It answers "why is my
// hashrate lower than with another miner" before anyone has to ask.
const kindnessWhyNote = "While you're using the computer, kind-miner mines on its efficiency cores at the Ghost ceiling, so you never feel it, and moves to every core once you've been away for the time set below. Full skips that wait, and mines on every core straight away, as dedicated miners such as Gupax do all the time."
