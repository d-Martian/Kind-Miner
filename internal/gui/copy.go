package gui

// Copy strings shown to users during onboarding. Kept here so the writing can
// be tuned without hunting through layout code.

// First-run setup, one constant per screen. The writing carries most of the
// weight here: these four screens are the only place a user is told what
// P2Pool is, why their own node is better, and what "kindness" buys them.
const (
	OnboardWalletTitle = "Where should your rewards go?"
	OnboardWalletBody  = "kind-miner mines on P2Pool: a peer-to-peer pool with no operator, no account and no fee. Your machine works alongside other miners, and when the group finds a block the reward is split by the work each miner did and paid straight to the addresses written into the block itself. Nobody holds your coins, including us."
	OnboardWalletNote  = "Use a primary address — it starts with 4 and is 95 characters. Subaddresses and integrated addresses cannot receive P2Pool payouts, because the reward arrives as a coinbase output."

	OnboardNodeTitle = "How should we reach the Monero network?"
	OnboardNodeBody  = "P2Pool needs block templates from a Monero node. Running your own is the private option and the one we'd pick, but it wants around 180 GB of disk and a day or two to sync."
	OnboardNodeNote  = "Our node is reached as a Tor hidden service, so the operator never learns your IP — but you are trusting our copy of the chain, and templates arrive a beat later."

	OnboardChainNote = "P2Pool has three sidechains, differing in how hard a share is to find. mini suits most desktops: you stay in the payout window continuously instead of earning in rare lumps. Pick nano below about 1 kH/s, main above roughly 50 kH/s. kind-miner will tell you if you outgrow the one you choose."

	OnboardBinariesTitle = "Which xmrig and p2pool should we run?"
	OnboardBinariesBody  = "kind-miner does not mine by itself — it supervises xmrig and p2pool, the two programs that do the work."
	OnboardBinariesNote  = "The bundled binaries are pinned to versions tested against this release, SHA256-verified before every start, and built reproducibly: compile them yourself from the pinned source and you get byte-identical files. kind-miner never runs a miner you did not choose, and never mines to any address but yours."

	OnboardKindnessTitle = "How kind should it be?"
	OnboardKindnessBody  = "Kindness sets how much of the machine the miner may take, and how fast it gets out of the way when you want it back. Every preset gives ground quickly and takes it back slowly — the difference is how much ground there is."
	OnboardKindnessNote  = "You can change this at any time from the tray, without stopping mining. Until you have been away from the keyboard for a few minutes, mining is held to the Ghost ceiling whatever you pick here."
)

// Wallet entry and its validation messages.
const (
	WalletPlaceholder = "4… or 8… (95 characters)"
	WalletHelp        = "Don't have a Monero wallet yet?"
	WalletHelpLink    = "Get one"
	WalletHelpURL     = "https://www.getmonero.org/downloads/"

	WalletErrEmpty   = "Address is required."
	WalletErrPrefix  = "Address must start with 4 or 8."
	WalletErrLength  = "Address should be 95 characters long."
	WalletErrCharset = "Address contains invalid characters."
	WalletOK         = "Looks like a valid Monero address."
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
