package worldgen

// bedrockChance is the share of bedrock from Y -64 up, measured on Bedrock: two solid layers, then
// falling by quarters (BEDROCK-NOTES section 9). Java's differs: 1, .8, .6, .4, .2.
var bedrockChance = [5]float64{1, 1, 0.75, 0.5, 0.25}
