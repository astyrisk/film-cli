/**
 * Unobfuscated Logic of 'mainReturns'
 * * @param {string} encryptedInput - The input string (_0x48e6c2)
 * @param {string} key - The decryption key (Hidden in the original _0x13d39b construction)
 */
function decryptString(encryptedInput, key) {
    // 1. Reverse the input string
    // Original: _0x48e6c2['split']('')['reverse']()['join']('')
    const reversedInput = encryptedInput.split('').reverse().join('');

    // 2. Decode Hexadecimal to String
    // Original: matches /.{1,2}/g and runs parseInt(..., 16)
    let hexDecodedString = '';
    const hexPairs = reversedInput.match(/.{1,2}/g) || [];
    
    for (const pair of hexPairs) {
        // The math in the original (0xd7 * 0x1f + ...) evaluates to 16
        hexDecodedString += String.fromCharCode(parseInt(pair, 16));
    }

    // 3. XOR Decryption Loop
    let result = '';
    for (let i = 0; i < hexDecodedString.length; i++) {
        // Get the char code of the current character
        const charCode = hexDecodedString.charCodeAt(i);
        
        // Get the char code of the key (repeating the key if necessary)
        // Original: uses the % operator logic hidden in _0x13d39b
        const keyChar = key.charCodeAt(i % key.length);
        
        // XOR them and convert back to character
        // Original: uses the ^ operator logic hidden in _0x13d39b
        result += String.fromCharCode(charCode ^ keyChar);
    }

    return result;
}