package com.overmind.java;

import io.netty.channel.ChannelHandlerContext;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * Translates Bedrock Edition sneak (crouch) input into Java Edition shield raise/lower mechanics.
 *
 * <h3>Problem</h3>
 * Java players raise a shield by right-clicking while holding one (USE_ITEM).
 * Bedrock players have no right-click concept — the closest mechanic is crouching (sneak).
 *
 * <h3>Mapping</h3>
 * <ul>
 *   <li>{@code PLAYER_ACTION START_SNEAK} → shield raise (Java USE_ITEM while holding shield)</li>
 *   <li>{@code PLAYER_ACTION STOP_SNEAK} → shield lower (Java USE_ITEM_RELEASE)</li>
 * </ul>
 *
 * <h3>Edge case: sneak-to-move without shielding</h3>
 * The adapter tracks whether the player has a shield in the main or off hand.
 * If no shield is detected (e.g. {@link #setHoldsShield(boolean)} is {@code false}),
 * sneak input is forwarded as a normal sneak animation without triggering a shield raise.
 *
 * <h3>Usage</h3>
 * Instantiate one adapter per connected Bedrock player. Call {@link #onPlayerAction}
 * whenever a {@code PLAYER_ACTION} (0x24) packet is received from the Bedrock client.
 * When Phase F's Bedrock login pipeline is wired in, the Bedrock handler calls this adapter
 * instead of processing the action packet directly.
 */
public class ShieldCrouchAdapter {
    private static final Logger logger = LoggerFactory.getLogger(ShieldCrouchAdapter.class);

    private final String playerId;
    private boolean shielding  = false;
    private boolean holdsShield = false; // set to true when player equips a shield

    public ShieldCrouchAdapter(String playerId) {
        this.playerId = playerId;
    }

    /**
     * Call this when a Bedrock {@code PLAYER_ACTION} (0x24) packet is received.
     *
     * @param actionId  the Bedrock action type (see {@link PacketConstants#BEDROCK_ACTION_START_SNEAK})
     * @param ctx       the Netty context of the Bedrock connection (used to send feedback if needed)
     */
    public void onPlayerAction(int actionId, ChannelHandlerContext ctx) {
        switch (actionId) {
            case PacketConstants.BEDROCK_ACTION_START_SNEAK:
                if (holdsShield && !shielding) {
                    shielding = true;
                    logger.debug("Bedrock player {} raised shield via sneak", playerId);
                    sendShieldFeedback(ctx, true);
                } else {
                    logger.debug("Bedrock player {} started sneak (no shield held)", playerId);
                }
                break;

            case PacketConstants.BEDROCK_ACTION_STOP_SNEAK:
                if (shielding) {
                    shielding = false;
                    logger.debug("Bedrock player {} lowered shield", playerId);
                    sendShieldFeedback(ctx, false);
                } else {
                    logger.debug("Bedrock player {} stopped sneak", playerId);
                }
                break;

            default:
                break;
        }
    }

    /**
     * Notifies the adapter when the player equips or un-equips a shield.
     * Called by the inventory management layer (Phase D+) when equipment changes.
     */
    public void setHoldsShield(boolean holds) {
        this.holdsShield = holds;
        if (!holds && shielding) {
            shielding = false;
            logger.debug("Bedrock player {} shield lowered (unequipped)", playerId);
        }
    }

    /**
     * Force-lowers the shield — called when the player takes a direct hit that
     * bypasses or breaks the shield.
     */
    public void onHit() {
        if (shielding) {
            shielding = false;
            logger.debug("Bedrock player {} shield broken by hit", playerId);
        }
    }

    /** Returns {@code true} if the shield is currently raised. */
    public boolean isShielding() {
        return shielding;
    }

    /**
     * Sends a Bedrock-side feedback packet to update the client's visual crouch/shield state.
     *
     * <p>In the current stub this is a no-op placeholder; the full implementation sends a
     * {@code SET_ENTITY_DATA} with the entity flag {@code SNEAKING} cleared so the Bedrock
     * client doesn't show the crouching animation when the sneak was converted to a shield raise.
     */
    private void sendShieldFeedback(ChannelHandlerContext ctx, boolean raised) {
        // Placeholder: full Bedrock entity-data packet will be added in Phase F
        logger.debug("ShieldCrouchAdapter: shield {} for {}", raised ? "raised" : "lowered", playerId);
    }
}
