package gui

// Copy strings shown to users during onboarding. Kept here so the writing can
// be tuned without hunting through layout code.

// Welcome screen.
const (
	WelcomeTitle    = "kind-miner"
	WelcomeSubtitle = "Earn Monero with your computer's idle CPU."
	WelcomeBody     = "Coins go straight to your wallet. No account. No fee. No middleman."
	WelcomeContinue = "Continue"
)

// Wallet entry screen.
const (
	WalletTitle      = "Where should we send your coins?"
	WalletPrompt     = "Paste your Monero wallet address below."
	WalletPlaceholder = "4… or 8… (95 characters)"
	WalletHelp       = "Don't have a Monero wallet yet?"
	WalletHelpLink   = "Get one"
	WalletHelpURL    = "https://www.getmonero.org/downloads/"
	WalletStart      = "Start mining"

	WalletErrEmpty    = "Address is required."
	WalletErrPrefix   = "Address must start with 4 or 8."
	WalletErrLength   = "Address should be 95 characters long."
	WalletErrCharset  = "Address contains invalid characters."
	WalletOK          = "Looks like a valid Monero address."
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
