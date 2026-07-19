(() => {
    "use strict";

    const root = document.getElementById("wallet-signin");
    const button = document.getElementById("wallet-connect");
    const status = document.getElementById("wallet-status");
    if (!root || !button || !status) return;

    const announcedProviders = new Map();

    const isProvider = (provider) => provider && typeof provider.request === "function";

    const isMetaMaskProvider = (provider) => (
        isProvider(provider) && provider.isMetaMask === true && provider.isPhantom !== true
    );

    const rememberProvider = (event) => {
        const detail = event && event.detail;
        const info = detail && detail.info;
        const provider = detail && detail.provider;
        if (!info || typeof info.uuid !== "string" || typeof info.rdns !== "string" || !isProvider(provider)) return;
        announcedProviders.set(info.uuid, { info, provider });
    };

    window.addEventListener("eip6963:announceProvider", rememberProvider);
    window.dispatchEvent(new Event("eip6963:requestProvider"));

    const findMetaMaskProvider = () => {
        for (const candidate of announcedProviders.values()) {
            if (candidate.info.rdns.toLowerCase() === "io.metamask" && isMetaMaskProvider(candidate.provider)) {
                return candidate.provider;
            }
        }

        const injected = window.ethereum;
        if (injected && Array.isArray(injected.providers)) {
            const provider = injected.providers.find(isMetaMaskProvider);
            if (provider) return provider;
        }
        return isMetaMaskProvider(injected) ? injected : null;
    };

    const setStatus = (message, state = "") => {
        status.textContent = message;
        status.className = `status${state ? ` ${state}` : ""}`;
    };

    const postJSON = async (url, body) => {
        const response = await fetch(url, {
            method: "POST",
            credentials: "same-origin",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(body),
        });
        let payload = {};
        try {
            payload = await response.json();
        } catch (_) {
            payload = {};
        }
        if (!response.ok) {
            if (payload.redirect_url) {
                window.location.assign(payload.redirect_url);
                return null;
            }
            const error = new Error(payload.error || "request_failed");
            error.code = payload.error || "request_failed";
            throw error;
        }
        return payload;
    };

    const utf8Hex = (value) => {
        const bytes = new TextEncoder().encode(value);
        return `0x${Array.from(bytes, byte => byte.toString(16).padStart(2, "0")).join("")}`;
    };

    const friendlyError = (error) => {
        if (error && (error.code === 4001 || error.code === "ACTION_REJECTED")) return "The wallet request was cancelled.";
        const messages = {
            invalid_origin: "The sign-in request did not come from this site.",
            rate_limited: "Too many attempts. Wait a few minutes and try again.",
            invalid_wallet: "MetaMask returned an invalid wallet address.",
            invalid_challenge: "The sign-in request expired or was already used.",
            invalid_signature: "The wallet signature could not be verified.",
            provider_unavailable: "MetaMask sign-in is currently unavailable.",
        };
        return messages[error && error.code] || "MetaMask sign-in could not be completed.";
    };

    button.addEventListener("click", async () => {
        window.dispatchEvent(new Event("eip6963:requestProvider"));
        const provider = findMetaMaskProvider();
        if (!provider) {
            setStatus("MetaMask browser extension was not detected.", "error");
            return;
        }
        button.disabled = true;
        try {
            setStatus("Waiting for MetaMask connection...");
            const accounts = await provider.request({ method: "eth_requestAccounts" });
            const address = Array.isArray(accounts) ? accounts[0] : "";
            if (!address) throw new Error("invalid_wallet");

            const currentChain = String(await provider.request({ method: "eth_chainId" })).toLowerCase();
            const expectedChain = String(root.dataset.chainIdHex || "").toLowerCase();
            if (currentChain !== expectedChain) {
                const error = new Error("wrong_chain");
                error.code = "wrong_chain";
                throw error;
            }

            setStatus("Preparing a one-time sign-in message...");
            const challenge = await postJSON(root.dataset.challengeUrl, {
                address,
                return_to: root.dataset.returnTo || "/",
            });
            if (!challenge) return;

            setStatus("Review and sign the message in MetaMask. No transaction will be sent.");
            const signature = await provider.request({
                method: "personal_sign",
                params: [utf8Hex(challenge.message), address],
            });

            setStatus("Verifying wallet signature...");
            const result = await postJSON(root.dataset.verifyUrl, {
                challenge_token: challenge.challenge_token,
                message: challenge.message,
                signature,
            });
            if (!result) return;
            setStatus("Wallet verified. Redirecting...", "success");
            window.location.assign(result.redirect_url || "/");
        } catch (error) {
            if (error && error.code === "wrong_chain") {
                setStatus(`Switch MetaMask to Chain ID ${root.dataset.chainId} and try again.`, "error");
            } else {
                setStatus(friendlyError(error), "error");
            }
        } finally {
            button.disabled = false;
        }
    });
})();
